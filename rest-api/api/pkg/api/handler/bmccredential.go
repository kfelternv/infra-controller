// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	tClient "go.temporal.io/sdk/client"

	"github.com/NVIDIA/infra-controller/rest-api/api/internal/config"
	"github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/handler/util/common"
	"github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/model"
	sc "github.com/NVIDIA/infra-controller/rest-api/api/pkg/client/site"
	auth "github.com/NVIDIA/infra-controller/rest-api/auth/pkg/authorization"
	cutil "github.com/NVIDIA/infra-controller/rest-api/common/pkg/util"
	cdb "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db"
	cdbm "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db/model"
)

// NICo Core (forge.Forge) credential method proxied by this handler.
const createCredentialMethod = "/forge.Forge/CreateCredential"

// bmcCredentialBase carries the shared dependencies and authorization for the
// BMC credential handlers. These are the first endpoints migrated to the
// generic NICo Core gRPC proxy (epic #1927): the handler stays curated
// (Provider Admin, site-scoped, validated) and forwards the typed request
// through the single generic proxy workflow instead of a bespoke one.
type bmcCredentialBase struct {
	dbSession  *cdb.Session
	scp        *sc.ClientPool
	cfg        *config.Config
	tracerSpan *cutil.TracerSpan
}

// authorizeSite validates the caller is a Provider Admin for org, resolves the
// site from the required ?siteId query parameter, and confirms it belongs to
// the org's Infrastructure Provider. Returns the per-site Temporal client. A
// non-nil error is the echo response the caller must return.
func (b bmcCredentialBase) authorizeSite(
	ctx context.Context,
	c echo.Context,
	logger zerolog.Logger,
	org string,
	dbUser *cdbm.User,
) (tClient.Client, string, error) {
	if dbUser == nil {
		logger.Error().Msg("invalid User object found in request context")
		return nil, "", cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve current user", nil)
	}

	ok, err := auth.ValidateOrgMembership(dbUser, org)
	if !ok {
		if err != nil {
			logger.Error().Err(err).Msg("error validating org membership for User in request")
		} else {
			logger.Warn().Msg("could not validate org membership for user, access denied")
		}
		return nil, "", cutil.NewAPIErrorResponse(c, http.StatusForbidden, fmt.Sprintf("Failed to validate membership for org: %s", org), nil)
	}

	// BMC credentials are an administrative secret-store operation: Provider Admin only.
	if ok := auth.ValidateUserRoles(dbUser, org, nil, auth.ProviderAdminRole); !ok {
		logger.Warn().Msg("user does not have Provider Admin role, access denied")
		return nil, "", cutil.NewAPIErrorResponse(c, http.StatusForbidden, "User does not have Provider Admin role with org", nil)
	}

	provider, err := common.GetInfrastructureProviderForOrg(ctx, nil, b.dbSession, org)
	if err != nil {
		logger.Warn().Err(err).Msg("error getting infrastructure provider for org")
		return nil, "", cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Failed to retrieve Infrastructure Provider for org", nil)
	}

	siteStrID := c.QueryParam("siteId")
	if siteStrID == "" {
		return nil, "", cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "siteId query parameter is required", nil)
	}

	site, err := common.GetSiteFromIDString(ctx, nil, siteStrID, b.dbSession)
	if err != nil {
		if errors.Is(err, cdb.ErrDoesNotExist) {
			return nil, "", cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Site specified in request does not exist", nil)
		}
		logger.Error().Err(err).Msg("error retrieving Site from DB")
		return nil, "", cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve Site due to DB error", nil)
	}

	if site.InfrastructureProviderID != provider.ID {
		return nil, "", cutil.NewAPIErrorResponse(c, http.StatusForbidden, "Site specified in request doesn't belong to current org's Provider", nil)
	}

	stc, err := b.scp.GetClientByID(site.ID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to retrieve Temporal client for Site")
		return nil, "", cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve client for Site", nil)
	}
	// The site ID is the shared key used to encrypt redacted secret fields for
	// transport to the site agent.
	return stc, site.ID.String(), nil
}

// ~~~~~ Set BMC credential ~~~~~ //

// SetBMCCredentialHandler stores (creates or overwrites) a BMC credential.
type SetBMCCredentialHandler struct {
	bmcCredentialBase
}

// NewSetBMCCredentialHandler returns a handler for setting a BMC credential.
func NewSetBMCCredentialHandler(dbSession *cdb.Session, scp *sc.ClientPool, cfg *config.Config) SetBMCCredentialHandler {
	return SetBMCCredentialHandler{
		bmcCredentialBase{dbSession: dbSession, scp: scp, cfg: cfg, tracerSpan: cutil.NewTracerSpan()},
	}
}

// Handle godoc
// @Summary Set a BMC credential (Provider Admin)
// @Description Store a site-wide or per-BMC root credential. Proxies to NICo Core CreateCredential. Equivalent to `carbide-admin-cli credential add-bmc`.
// @Tags bmc-credential
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param org path string true "Name of NGC organization"
// @Param siteId query string true "ID of the Site"
// @Param request body model.APIBMCCredentialRequest true "BMC credential"
// @Success 204 "No Content"
// @Router /v2/org/{org}/nico/credential/bmc [put]
func (h SetBMCCredentialHandler) Handle(c echo.Context) error {
	org, dbUser, ctx, logger, handlerSpan := common.SetupHandler("BMCCredential", "Set", c, h.tracerSpan)
	if handlerSpan != nil {
		defer handlerSpan.End()
	}

	var apiReq model.APIBMCCredentialRequest
	if err := c.Bind(&apiReq); err != nil {
		return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Invalid request body", nil)
	}
	if err := apiReq.Validate(); err != nil {
		return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, err.Error(), nil)
	}

	stc, siteID, errResp := h.authorizeSite(ctx, c, logger, org, dbUser)
	if errResp != nil {
		return errResp
	}

	// Do not log the request: it contains the credential password.
	logger.Info().Str("kind", apiReq.Kind).Msg("setting BMC credential via Core proxy")

	// "password" is redacted from the Temporal payload and carried encrypted.
	code, err := common.ExecuteCoreGRPC(ctx, stc, createCredentialMethod, apiReq.ToProto(), nil, siteID, "password")
	if err != nil {
		logger.Error().Err(err).Msg("failed to set BMC credential")
		return cutil.NewAPIErrorResponse(c, code, "Failed to set BMC credential", nil)
	}

	return c.NoContent(http.StatusNoContent)
}
