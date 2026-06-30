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

type ListMachineHealthReportHandler struct {
	adminMachineBase
}

func NewListMachineHealthReportHandler(dbSession *cdb.Session, scp *sc.ClientPool, cfg *config.Config) ListMachineHealthReportHandler {
	return ListMachineHealthReportHandler{
		adminMachineBase{dbSession: dbSession, scp: scp, cfg: cfg, tracerSpan: cutil.NewTracerSpan()},
	}
}

// Handle godoc
// @Summary List Machine Health Reports
// @Description List Machine health report overrides through NICo Core. Provider Admin only.
// @Tags health-report
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param org path string true "Name of NGC organization"
// @Param machineId path string true "ID of Machine"
// @Success 200 {object} model.APIMachineHealthReportListResponse
// @Router /v2/org/{org}/nico/machine/{machineId}/health-report [get]
func (h ListMachineHealthReportHandler) Handle(c echo.Context) error {
	org, dbUser, ctx, logger, handlerSpan := common.SetupHandler("MachineHealthReport", "List", c, h.tracerSpan)
	if handlerSpan != nil {
		defer handlerSpan.End()
	}

	machineID := c.Param("machineId")
	stc, siteID, _, errResp := h.authorizeMachine(ctx, c, logger, org, dbUser, machineID)
	if errResp != nil || stc == nil {
		return errResp
	}

	logger.Info().Str("machineID", machineID).Str("siteID", siteID).Msg("listing Machine health reports via Core proxy")
	coreResp := &cwssaws.ListHealthReportResponse{}
	code, err := common.ExecuteCoreGRPC(ctx, stc, cwssaws.Forge_ListMachineHealthReports_FullMethodName, model.NewMachineIDProto(machineID), coreResp, siteID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list Machine health reports")
		return cutil.NewAPIErrorResponse(c, code, "Failed to list Machine health reports", nil)
	}

	return c.JSON(http.StatusOK, model.NewAPIMachineHealthReportListResponse(machineID, coreResp))
}

type InsertMachineHealthReportHandler struct {
	adminMachineBase
}

func NewInsertMachineHealthReportHandler(dbSession *cdb.Session, scp *sc.ClientPool, cfg *config.Config) InsertMachineHealthReportHandler {
	return InsertMachineHealthReportHandler{
		adminMachineBase{dbSession: dbSession, scp: scp, cfg: cfg, tracerSpan: cutil.NewTracerSpan()},
	}
}

// Handle godoc
// @Summary Insert Machine Health Report
// @Description Add or update a Machine health report override through NICo Core. Provider Admin only.
// @Tags health-report
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param org path string true "Name of NGC organization"
// @Param machineId path string true "ID of Machine"
// @Param request body model.APIMachineHealthReportEntry true "Machine health report"
// @Success 200 {object} model.APIMachineHealthReportResponse
// @Router /v2/org/{org}/nico/machine/{machineId}/health-report [put]
func (h InsertMachineHealthReportHandler) Handle(c echo.Context) error {
	org, dbUser, ctx, logger, handlerSpan := common.SetupHandler("MachineHealthReport", "Insert", c, h.tracerSpan)
	if handlerSpan != nil {
		defer handlerSpan.End()
	}

	machineID := c.Param("machineId")
	var apiReq model.APIMachineHealthReportEntry
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

	logger.Info().Str("machineID", machineID).Str("source", apiReq.Source).Str("siteID", siteID).Msg("inserting Machine health report via Core proxy")
	code, err := common.ExecuteCoreGRPC(ctx, stc, cwssaws.Forge_InsertMachineHealthReport_FullMethodName, apiReq.ToProto(machineID), nil, siteID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to insert Machine health report")
		return cutil.NewAPIErrorResponse(c, code, "Failed to insert Machine health report", nil)
	}

	return c.JSON(http.StatusOK, model.NewAPIMachineHealthReportResponse(machineID, &apiReq))
}

type RemoveMachineHealthReportHandler struct {
	adminMachineBase
}

func NewRemoveMachineHealthReportHandler(dbSession *cdb.Session, scp *sc.ClientPool, cfg *config.Config) RemoveMachineHealthReportHandler {
	return RemoveMachineHealthReportHandler{
		adminMachineBase{dbSession: dbSession, scp: scp, cfg: cfg, tracerSpan: cutil.NewTracerSpan()},
	}
}

// Handle godoc
// @Summary Remove Machine Health Report
// @Description Remove a Machine health report override through NICo Core. Provider Admin only.
// @Tags health-report
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param org path string true "Name of NGC organization"
// @Param machineId path string true "ID of Machine"
// @Param source path string true "Health report source"
// @Success 200 {object} model.APIMachineHealthReportRemoveResponse
// @Router /v2/org/{org}/nico/machine/{machineId}/health-report/{source} [delete]
func (h RemoveMachineHealthReportHandler) Handle(c echo.Context) error {
	org, dbUser, ctx, logger, handlerSpan := common.SetupHandler("MachineHealthReport", "Remove", c, h.tracerSpan)
	if handlerSpan != nil {
		defer handlerSpan.End()
	}

	machineID := c.Param("machineId")
	source := c.Param("source")
	if source == "" {
		return cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "source is required", nil)
	}

	stc, siteID, _, errResp := h.authorizeMachine(ctx, c, logger, org, dbUser, machineID)
	if errResp != nil || stc == nil {
		return errResp
	}

	logger.Info().Str("machineID", machineID).Str("source", source).Str("siteID", siteID).Msg("removing Machine health report via Core proxy")
	code, err := common.ExecuteCoreGRPC(ctx, stc, cwssaws.Forge_RemoveMachineHealthReport_FullMethodName, model.NewRemoveMachineHealthReportProto(machineID, source), nil, siteID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to remove Machine health report")
		return cutil.NewAPIErrorResponse(c, code, "Failed to remove Machine health report", nil)
	}

	return c.JSON(http.StatusOK, model.NewAPIMachineHealthReportRemoveResponse(machineID, source))
}
