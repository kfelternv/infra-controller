// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/NVIDIA/infra-controller/rest-api/api/internal/config"
	"github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/handler/util/common"
	"github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/model"
	sc "github.com/NVIDIA/infra-controller/rest-api/api/pkg/client/site"
	cutil "github.com/NVIDIA/infra-controller/rest-api/common/pkg/util"
	cdb "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db"
	cdbm "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db/model"
	cwssaws "github.com/NVIDIA/infra-controller/rest-api/workflow-schema/schema/site-agent/workflows/v1"
)

type DpuReprovisionHandler struct {
	dbSession  *cdb.Session
	scp        *sc.ClientPool
	tracerSpan *cutil.TracerSpan
}

func NewDpuReprovisionHandler(dbSession *cdb.Session, scp *sc.ClientPool, _ *config.Config) DpuReprovisionHandler {
	return DpuReprovisionHandler{
		dbSession:  dbSession,
		scp:        scp,
		tracerSpan: cutil.NewTracerSpan(),
	}
}

// Handle godoc
// @Summary Trigger DPU Reprovisioning
// @Description Trigger DPU reprovisioning for a Machine through NICo Core. Provider Admin only.
// @Tags dpu-reprovision
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param org path string true "Name of NGC organization"
// @Param machineId path string true "ID of Machine"
// @Param request body model.APIDpuReprovisionRequest true "DPU reprovision request"
// @Success 200 {object} model.APIDpuReprovisionResponse
// @Router /v2/org/{org}/nico/machine/{machineId}/dpu-reprovision [patch]
func (h DpuReprovisionHandler) Handle(c echo.Context) error {
	org, dbUser, ctx, logger, handlerSpan := common.SetupHandler("DpuReprovision", "Trigger", c, h.tracerSpan)
	if handlerSpan != nil {
		defer handlerSpan.End()
	}
	if dbUser == nil {
		logger.Error().Msg("invalid User object found in request context")
		return cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve current user", nil)
	}

	machineID := c.Param("machineId")
	if machineID == "" {
		return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Machine ID is required", nil)
	}

	var apiReq model.APIDpuReprovisionRequest
	if err := c.Bind(&apiReq); err != nil {
		return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Invalid request body", nil)
	}
	if err := apiReq.Validate(); err != nil {
		return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, err.Error(), nil)
	}
	apiReq.MachineID = machineID

	provider, apiErr := common.IsProvider(ctx, logger, h.dbSession, org, dbUser, false)
	if apiErr != nil {
		return cutil.NewAPIErrorResponse(c, apiErr.Code, apiErr.Message, apiErr.Data)
	}

	machine, err := cdbm.NewMachineDAO(h.dbSession).GetByID(ctx, nil, machineID, nil, false)
	if err != nil {
		if errors.Is(err, cdb.ErrDoesNotExist) {
			return cutil.NewAPIErrorResponse(c, http.StatusNotFound, "Could not find Machine with specified ID", nil)
		}
		logger.Error().Err(err).Msg("error retrieving Machine DB entity")
		return cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Could not retrieve Machine", nil)
	}
	if machine.InfrastructureProviderID != provider.ID {
		return cutil.NewAPIErrorResponse(c, http.StatusForbidden, "Machine doesn't belong to org's Infrastructure provider", nil)
	}

	site, err := common.GetSiteFromIDString(ctx, nil, machine.SiteID.String(), h.dbSession)
	if err != nil {
		if errors.Is(err, cdb.ErrDoesNotExist) || errors.Is(err, common.ErrInvalidID) {
			return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Machine Site does not exist", nil)
		}
		logger.Error().Err(err).Msg("error retrieving Machine Site from DB")
		return cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve Machine Site due to DB error", nil)
	}
	if site.InfrastructureProviderID != provider.ID {
		return cutil.NewAPIErrorResponse(c, http.StatusForbidden, "Machine Site doesn't belong to current org's Provider", nil)
	}

	stc, err := h.scp.GetClientByID(site.ID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to retrieve Temporal client for Site")
		return cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve client for Site", nil)
	}
	siteID := site.ID.String()

	logger.Info().Str("machineID", machineID).Str("mode", apiReq.Mode).Str("siteID", siteID).Msg("triggering DPU reprovisioning via Core proxy")
	code, err := common.ExecuteCoreGRPC(ctx, stc, cwssaws.Forge_TriggerDpuReprovisioning_FullMethodName, apiReq.ToProto(), nil, siteID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to trigger DPU reprovisioning")
		return cutil.NewAPIErrorResponse(c, code, "Failed to trigger DPU reprovisioning", nil)
	}

	return c.JSON(http.StatusOK, model.NewAPIDpuReprovisionResponse(&apiReq))
}
