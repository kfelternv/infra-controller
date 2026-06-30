// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"errors"
	"io"
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

type BmcResetHandler struct {
	adminMachineBase
}

func NewBmcResetHandler(dbSession *cdb.Session, scp *sc.ClientPool, cfg *config.Config) BmcResetHandler {
	return BmcResetHandler{
		adminMachineBase{dbSession: dbSession, scp: scp, cfg: cfg, tracerSpan: cutil.NewTracerSpan()},
	}
}

// Handle godoc
// @Summary Reset Machine BMC
// @Description Reset a Machine BMC through NICo Core. Provider Admin only.
// @Tags bmc-reset
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param org path string true "Name of NGC organization"
// @Param machineId path string true "ID of Machine"
// @Param request body model.APIBmcResetRequest true "BMC reset request"
// @Success 200 {object} model.APIBmcResetResponse
// @Router /v2/org/{org}/nico/machine/{machineId}/bmc-reset [post]
func (h BmcResetHandler) Handle(c echo.Context) error {
	org, dbUser, ctx, logger, handlerSpan := common.SetupHandler("BmcReset", "Reset", c, h.tracerSpan)
	if handlerSpan != nil {
		defer handlerSpan.End()
	}

	machineID := c.Param("machineId")
	var apiReq model.APIBmcResetRequest
	if c.Request().ContentLength != 0 {
		if err := c.Bind(&apiReq); err != nil {
			if !errors.Is(err, io.EOF) {
				return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Invalid request body", nil)
			}
		}
	}
	if err := apiReq.Validate(); err != nil {
		return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, err.Error(), nil)
	}

	stc, siteID, _, errResp := h.authorizeMachine(ctx, c, logger, org, dbUser, machineID)
	if errResp != nil || stc == nil {
		return errResp
	}

	logger.Info().Str("machineID", machineID).Str("siteID", siteID).Bool("useIpmitool", apiReq.UseIpmitool).Msg("resetting Machine BMC via Core proxy")
	coreResp := &cwssaws.AdminBmcResetResponse{}
	code, err := common.ExecuteCoreGRPC(ctx, stc, cwssaws.Forge_AdminBmcReset_FullMethodName, apiReq.ToProto(machineID), coreResp, siteID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to reset Machine BMC")
		return cutil.NewAPIErrorResponse(c, code, "Failed to reset Machine BMC", nil)
	}

	return c.JSON(http.StatusOK, model.NewAPIBmcResetResponse(machineID, &apiReq))
}
