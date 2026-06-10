// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	temporalEnums "go.temporal.io/api/enums/v1"
	tClient "go.temporal.io/sdk/client"
	tp "go.temporal.io/sdk/temporal"

	"github.com/NVIDIA/infra-controller/rest-api/api/internal/config"
	"github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/handler/util/common"
	"github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/model"
	sc "github.com/NVIDIA/infra-controller/rest-api/api/pkg/client/site"
	auth "github.com/NVIDIA/infra-controller/rest-api/auth/pkg/authorization"
	npt "github.com/NVIDIA/infra-controller/rest-api/common/pkg/nicopassthrough"
	cutil "github.com/NVIDIA/infra-controller/rest-api/common/pkg/util"
	cdb "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db"
	cdbm "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db/model"
)

// nicoCorePassthroughBase carries the shared dependencies and authorization
// logic for the NICo Core gRPC passthrough handlers.
//
// The passthrough exposes the full NICo Core (forge.Forge) gRPC surface to
// Provider Admins through a single mechanism instead of a hand-written REST
// endpoint per operation. The request is gated here, then run on the site's
// Flow worker — which holds the direct mutual-TLS gRPC connection to Core — via
// a Temporal workflow.
type nicoCorePassthroughBase struct {
	dbSession  *cdb.Session
	scp        *sc.ClientPool
	cfg        *config.Config
	tracerSpan *cutil.TracerSpan
}

// authorize validates the caller is a Provider Admin for org, resolves the site
// from the required ?siteId query parameter, confirms it belongs to the org's
// Infrastructure Provider and has NICo Flow enabled, and returns the per-site
// Temporal client. A non-nil error is the echo response the caller must return.
func (b nicoCorePassthroughBase) authorize(
	ctx context.Context,
	c echo.Context,
	logger zerolog.Logger,
	org string,
	dbUser *cdbm.User,
) (tClient.Client, error) {
	if dbUser == nil {
		logger.Error().Msg("invalid User object found in request context")
		return nil, cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve current user", nil)
	}

	ok, err := auth.ValidateOrgMembership(dbUser, org)
	if !ok {
		if err != nil {
			logger.Error().Err(err).Msg("error validating org membership for User in request")
		} else {
			logger.Warn().Msg("could not validate org membership for user, access denied")
		}
		return nil, cutil.NewAPIErrorResponse(c, http.StatusForbidden, fmt.Sprintf("Failed to validate membership for org: %s", org), nil)
	}

	// The passthrough is an internal/break-glass surface: Provider Admin only.
	if ok := auth.ValidateUserRoles(dbUser, org, nil, auth.ProviderAdminRole); !ok {
		logger.Warn().Msg("user does not have Provider Admin role, access denied")
		return nil, cutil.NewAPIErrorResponse(c, http.StatusForbidden, "User does not have Provider Admin role with org", nil)
	}

	provider, err := common.GetInfrastructureProviderForOrg(ctx, nil, b.dbSession, org)
	if err != nil {
		logger.Warn().Err(err).Msg("error getting infrastructure provider for org")
		return nil, cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Failed to retrieve Infrastructure Provider for org", nil)
	}

	siteStrID := c.QueryParam("siteId")
	if siteStrID == "" {
		return nil, cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "siteId query parameter is required", nil)
	}

	site, err := common.GetSiteFromIDString(ctx, nil, siteStrID, b.dbSession)
	if err != nil {
		if errors.Is(err, cdb.ErrDoesNotExist) {
			return nil, cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Site specified in request does not exist", nil)
		}
		logger.Error().Err(err).Msg("error retrieving Site from DB")
		return nil, cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve Site due to DB error", nil)
	}

	if site.InfrastructureProviderID != provider.ID {
		return nil, cutil.NewAPIErrorResponse(c, http.StatusForbidden, "Site specified in request doesn't belong to current org's Provider", nil)
	}

	siteConfig := &cdbm.SiteConfig{}
	if site.Config != nil {
		siteConfig = site.Config
	}
	if !siteConfig.Flow {
		logger.Warn().Msg("site does not have NICo Flow enabled")
		return nil, cutil.NewAPIErrorResponse(c, http.StatusPreconditionFailed, "Site does not have NICo Flow enabled", nil)
	}

	stc, err := b.scp.GetClientByID(site.ID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to retrieve Temporal client for Site")
		return nil, cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve client for Site", nil)
	}
	return stc, nil
}

// runWorkflow starts the passthrough workflow on the site's Flow task queue and
// returns its result. A non-nil error is the echo response to return.
func (b nicoCorePassthroughBase) runWorkflow(
	ctx context.Context,
	c echo.Context,
	logger zerolog.Logger,
	stc tClient.Client,
	req npt.Request,
) (npt.Response, error) {
	workflowID := fmt.Sprintf("nico-core-passthrough-%s-%s", npt.MethodName(req.Method), uuid.NewString())
	if req.List {
		workflowID = fmt.Sprintf("nico-core-passthrough-methods-%s", uuid.NewString())
	}

	workflowOptions := tClient.StartWorkflowOptions{
		ID:                       workflowID,
		WorkflowExecutionTimeout: cutil.WorkflowExecutionTimeout,
		TaskQueue:                npt.TaskQueue,
		WorkflowIDReusePolicy:    temporalEnums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}

	wfCtx, cancel := context.WithTimeout(ctx, cutil.WorkflowContextTimeout)
	defer cancel()

	we, err := stc.ExecuteWorkflow(wfCtx, workflowOptions, npt.WorkflowName, req)
	if err != nil {
		logger.Error().Err(err).Msg("failed to execute NICo Core passthrough workflow")
		return npt.Response{}, cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to execute NICo Core passthrough", nil)
	}

	var resp npt.Response
	if err := we.Get(wfCtx, &resp); err != nil {
		var timeoutErr *tp.TimeoutError
		if errors.As(err, &timeoutErr) || errors.Is(err, context.DeadlineExceeded) || wfCtx.Err() != nil {
			return npt.Response{}, common.TerminateWorkflowOnTimeOut(c, logger, stc, workflowID, err, "NICoCorePassthrough", npt.WorkflowName)
		}
		code, werr := common.UnwrapWorkflowError(err)
		logger.Error().Err(werr).Msg("NICo Core passthrough workflow failed")
		return npt.Response{}, cutil.NewAPIErrorResponse(c, code, fmt.Sprintf("NICo Core passthrough failed: %s", werr), nil)
	}
	return resp, nil
}

// ~~~~~ Invoke handler ~~~~~ //

// InvokeNICoCorePassthroughHandler invokes a single NICo Core gRPC method.
type InvokeNICoCorePassthroughHandler struct {
	nicoCorePassthroughBase
}

// NewInvokeNICoCorePassthroughHandler returns a handler for invoking NICo Core
// methods through the passthrough.
func NewInvokeNICoCorePassthroughHandler(dbSession *cdb.Session, scp *sc.ClientPool, cfg *config.Config) InvokeNICoCorePassthroughHandler {
	return InvokeNICoCorePassthroughHandler{
		nicoCorePassthroughBase{dbSession: dbSession, scp: scp, cfg: cfg, tracerSpan: cutil.NewTracerSpan()},
	}
}

// Handle godoc
// @Summary Invoke a NICo Core gRPC method (Provider Admin)
// @Description Invoke any NICo Core (forge.Forge) gRPC method by name with a protojson request. Read methods require Provider Admin; mutations additionally require allowMutation=true.
// @Tags nico-core-passthrough
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param org path string true "Name of NGC organization"
// @Param siteId query string true "ID of the Site"
// @Param request body model.APINICoCorePassthroughRequest true "Method and protojson request"
// @Success 200 {object} model.APINICoCorePassthroughResponse
// @Router /v2/org/{org}/nico/core/passthrough [post]
func (h InvokeNICoCorePassthroughHandler) Handle(c echo.Context) error {
	org, dbUser, ctx, logger, handlerSpan := common.SetupHandler("NICoCorePassthrough", "Invoke", c, h.tracerSpan)
	if handlerSpan != nil {
		defer handlerSpan.End()
	}

	var apiReq model.APINICoCorePassthroughRequest
	if err := c.Bind(&apiReq); err != nil {
		return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Invalid request body", nil)
	}
	if apiReq.Method == "" {
		return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "method is required", nil)
	}

	stc, errResp := h.authorize(ctx, c, logger, org, dbUser)
	if errResp != nil {
		return errResp
	}

	// Gate write/destructive methods behind an explicit opt-in, keeping
	// break-glass mutations separate from read parity (epic #1927).
	if npt.IsMutation(apiReq.Method) && !apiReq.AllowMutation {
		return cutil.NewAPIErrorResponse(
			c, http.StatusForbidden,
			fmt.Sprintf("Method %q is a mutation; set allowMutation=true to permit it", npt.MethodName(apiReq.Method)),
			nil,
		)
	}

	resp, errResp := h.runWorkflow(ctx, c, logger, stc, npt.Request{
		Method:        apiReq.Method,
		RequestJSON:   apiReq.Request,
		AllowMutation: apiReq.AllowMutation,
	})
	if errResp != nil {
		return errResp
	}

	return c.JSON(http.StatusOK, model.APINICoCorePassthroughResponse{
		Method:   npt.MethodName(apiReq.Method),
		Response: resp.ResponseJSON,
	})
}

// ~~~~~ List methods handler ~~~~~ //

// ListNICoCorePassthroughMethodsHandler returns the catalog of invocable NICo
// Core methods.
type ListNICoCorePassthroughMethodsHandler struct {
	nicoCorePassthroughBase
}

// NewListNICoCorePassthroughMethodsHandler returns a handler for listing the
// NICo Core method catalog.
func NewListNICoCorePassthroughMethodsHandler(dbSession *cdb.Session, scp *sc.ClientPool, cfg *config.Config) ListNICoCorePassthroughMethodsHandler {
	return ListNICoCorePassthroughMethodsHandler{
		nicoCorePassthroughBase{dbSession: dbSession, scp: scp, cfg: cfg, tracerSpan: cutil.NewTracerSpan()},
	}
}

// Handle godoc
// @Summary List invocable NICo Core gRPC methods (Provider Admin)
// @Description Return the catalog of NICo Core (forge.Forge) unary methods, each with its request/response message types and read/mutation classification.
// @Tags nico-core-passthrough
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param org path string true "Name of NGC organization"
// @Param siteId query string true "ID of the Site"
// @Success 200 {object} model.APINICoCoreMethodsResponse
// @Router /v2/org/{org}/nico/core/methods [get]
func (h ListNICoCorePassthroughMethodsHandler) Handle(c echo.Context) error {
	org, dbUser, ctx, logger, handlerSpan := common.SetupHandler("NICoCorePassthrough", "ListMethods", c, h.tracerSpan)
	if handlerSpan != nil {
		defer handlerSpan.End()
	}

	stc, errResp := h.authorize(ctx, c, logger, org, dbUser)
	if errResp != nil {
		return errResp
	}

	resp, errResp := h.runWorkflow(ctx, c, logger, stc, npt.Request{List: true})
	if errResp != nil {
		return errResp
	}

	return c.JSON(http.StatusOK, model.APINICoCoreMethodsResponse{
		Service: npt.ServiceName,
		Methods: resp.Methods,
	})
}
