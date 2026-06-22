// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	tmocks "go.temporal.io/sdk/mocks"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/handler/util/common"
	"github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/model"
	sc "github.com/NVIDIA/infra-controller/rest-api/api/pkg/client/site"
	authz "github.com/NVIDIA/infra-controller/rest-api/auth/pkg/authorization"
	"github.com/NVIDIA/infra-controller/rest-api/common/pkg/coreproxy"
	cwssaws "github.com/NVIDIA/infra-controller/rest-api/workflow-schema/schema/site-agent/workflows/v1"
)

func TestCreateOrUpdateBMCCredentialHandlerClearsMacAddressForSiteWideRoot(t *testing.T) {
	dbSession := common.TestInitDB(t)
	defer dbSession.Close()

	common.TestSetupSchema(t, dbSession)

	org := "test-org"
	user := common.TestBuildUser(t, dbSession, "test-starfleet-id", org, []string{authz.ProviderAdminRole})
	ip := common.TestBuildInfrastructureProvider(t, dbSession, "Test Infrastructure Provider", org, user)
	site := common.TestBuildSite(t, dbSession, ip, "Test Site", user)

	cfg := common.GetTestConfig()
	tcfg, err := cfg.GetTemporalConfig()
	require.NoError(t, err)
	scp := sc.NewClientPool(tcfg)

	var proxiedReq coreproxy.Request
	wrun := &tmocks.WorkflowRun{}
	wrun.On("Get", mock.Anything, mock.Anything).Return(nil)

	tsc := &tmocks.Client{}
	tsc.On(
		"ExecuteWorkflow",
		mock.Anything,
		mock.Anything,
		coreproxy.WorkflowName,
		mock.MatchedBy(func(req coreproxy.Request) bool {
			proxiedReq = req
			return true
		}),
	).Return(wrun, nil)
	scp.IDClientMap[site.ID.String()] = tsc

	mac := "aa:bb:cc:dd:ee:ff"
	body, err := json.Marshal(model.APIBMCCredentialRequest{
		SiteID:     site.ID.String(),
		Kind:       model.BMCCredentialKindSiteWideRoot,
		Password:   "secret-password",
		MacAddress: &mac,
	})
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ec := e.NewContext(req, rec)
	ec.SetParamNames("orgName")
	ec.SetParamValues(org)
	ec.Set("user", user)

	handler := NewCreateOrUpdateBMCCredentialHandler(dbSession, scp, cfg)
	require.NoError(t, handler.Handle(ec))
	assert.Equal(t, http.StatusOK, rec.Code)

	var coreReq cwssaws.CredentialCreationRequest
	require.NoError(t, protojson.Unmarshal(proxiedReq.RequestJSON, &coreReq))
	assert.Equal(t, cwssaws.CredentialType_SiteWideBmcRoot, coreReq.GetCredentialType())
	assert.Nil(t, coreReq.MacAddress)
	assert.NotContains(t, string(proxiedReq.RequestJSON), "macAddress")

	var resp model.APIBMCCredential
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, site.ID.String(), resp.SiteID)
	assert.Equal(t, model.BMCCredentialKindSiteWideRoot, resp.Kind)
	assert.Nil(t, resp.MacAddress)
	assert.NotContains(t, rec.Body.String(), "macAddress")
}
