// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/NVIDIA/infra-controller/rest-api/api/internal/config"
	"github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/handler/util/common"
	"github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/model"
	sc "github.com/NVIDIA/infra-controller/rest-api/api/pkg/client/site"
	cutil "github.com/NVIDIA/infra-controller/rest-api/common/pkg/util"
	cdb "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db"
	cwssaws "github.com/NVIDIA/infra-controller/rest-api/workflow-schema/schema/site-agent/workflows/v1"
)

type DpuReprovisionHandler struct {
	adminMachineBase
}

func NewDpuReprovisionHandler(dbSession *cdb.Session, scp *sc.ClientPool, cfg *config.Config) DpuReprovisionHandler {
	return DpuReprovisionHandler{
		adminMachineBase{dbSession: dbSession, scp: scp, cfg: cfg, tracerSpan: cutil.NewTracerSpan()},
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

	machineID := c.Param("machineId")
	var apiReq model.APIDpuReprovisionRequest
	if err := c.Bind(&apiReq); err != nil {
		return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Invalid request body", nil)
	}
	if err := apiReq.Validate(); err != nil {
		return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, err.Error(), nil)
	}

	stc, siteID, _, errResp := h.authorizeMachine(ctx, c, logger, org, dbUser, machineID)
	if errResp != nil || stc == nil {
		return errResp
	}

	logger.Info().Str("machineID", machineID).Str("mode", apiReq.Mode).Str("siteID", siteID).Msg("triggering DPU reprovisioning via Core proxy")
	code, err := common.ExecuteCoreGRPC(ctx, stc, cwssaws.Forge_TriggerDpuReprovisioning_FullMethodName, apiReq.ToProto(machineID), nil, siteID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to trigger DPU reprovisioning")
		return cutil.NewAPIErrorResponse(c, code, "Failed to trigger DPU reprovisioning", nil)
	}

	return c.JSON(http.StatusOK, model.NewAPIDpuReprovisionResponse(machineID, &apiReq))
}
