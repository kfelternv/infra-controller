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
	sc "github.com/NVIDIA/infra-controller/rest-api/api/pkg/client/site"
	auth "github.com/NVIDIA/infra-controller/rest-api/auth/pkg/authorization"
	cutil "github.com/NVIDIA/infra-controller/rest-api/common/pkg/util"
	cdb "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db"
	cdbm "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db/model"
)

type adminMachineBase struct {
	dbSession  *cdb.Session
	scp        *sc.ClientPool
	cfg        *config.Config
	tracerSpan *cutil.TracerSpan
}

func (b adminMachineBase) authorizeMachine(
	ctx context.Context,
	c echo.Context,
	logger zerolog.Logger,
	org string,
	dbUser *cdbm.User,
	machineID string,
) (tClient.Client, string, *cdbm.Machine, error) {
	if dbUser == nil {
		logger.Error().Msg("invalid User object found in request context")
		return nil, "", nil, cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve current user", nil)
	}
	if machineID == "" {
		return nil, "", nil, cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Machine ID is required", nil)
	}

	ok, err := auth.ValidateOrgMembership(dbUser, org)
	if !ok {
		if err != nil {
			logger.Error().Err(err).Msg("error validating org membership for User in request")
		} else {
			logger.Warn().Msg("could not validate org membership for user, access denied")
		}
		return nil, "", nil, cutil.NewAPIErrorResponse(c, http.StatusForbidden, fmt.Sprintf("Failed to validate membership for org: %s", org), nil)
	}

	if ok := auth.ValidateUserRoles(dbUser, org, nil, auth.ProviderAdminRole); !ok {
		logger.Warn().Msg("user does not have Provider Admin role, access denied")
		return nil, "", nil, cutil.NewAPIErrorResponse(c, http.StatusForbidden, "User does not have Provider Admin role with org", nil)
	}

	provider, err := common.GetInfrastructureProviderForOrg(ctx, nil, b.dbSession, org)
	if err != nil {
		logger.Warn().Err(err).Msg("error getting infrastructure provider for org")
		return nil, "", nil, cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Failed to retrieve Infrastructure Provider for org", nil)
	}

	machine, err := cdbm.NewMachineDAO(b.dbSession).GetByID(ctx, nil, machineID, nil, false)
	if err != nil {
		if errors.Is(err, cdb.ErrDoesNotExist) {
			return nil, "", nil, cutil.NewAPIErrorResponse(c, http.StatusNotFound, "Could not find Machine with specified ID", nil)
		}
		logger.Error().Err(err).Msg("error retrieving Machine DB entity")
		return nil, "", nil, cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Could not retrieve Machine", nil)
	}

	if machine.InfrastructureProviderID != provider.ID {
		return nil, "", nil, cutil.NewAPIErrorResponse(c, http.StatusForbidden, "Machine doesn't belong to org's Infrastructure provider", nil)
	}

	site, err := common.GetSiteFromIDString(ctx, nil, machine.SiteID.String(), b.dbSession)
	if err != nil {
		if errors.Is(err, cdb.ErrDoesNotExist) || errors.Is(err, common.ErrInvalidID) {
			return nil, "", nil, cutil.NewAPIErrorResponse(c, http.StatusBadRequest, "Machine Site does not exist", nil)
		}
		logger.Error().Err(err).Msg("error retrieving Machine Site from DB")
		return nil, "", nil, cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve Machine Site due to DB error", nil)
	}
	if site.InfrastructureProviderID != provider.ID {
		return nil, "", nil, cutil.NewAPIErrorResponse(c, http.StatusForbidden, "Machine Site doesn't belong to current org's Provider", nil)
	}

	stc, err := b.scp.GetClientByID(site.ID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to retrieve Temporal client for Site")
		return nil, "", nil, cutil.NewAPIErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve client for Site", nil)
	}
	return stc, site.ID.String(), machine, nil
}
