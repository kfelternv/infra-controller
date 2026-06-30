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
	cutil "github.com/NVIDIA/infra-controller/rest-api/common/pkg/util"
	cdbm "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db/model"
	cwssaws "github.com/NVIDIA/infra-controller/rest-api/workflow-schema/schema/site-agent/workflows/v1"
)

type bmcResetHandlerFixture struct {
	org        string
	siteID     string
	machineID  string
	user       interface{}
	handler    echo.HandlerFunc
	proxiedReq *coreproxy.Request
}

func newBmcResetHandlerFixture(t *testing.T) bmcResetHandlerFixture {
	t.Helper()

	dbSession := common.TestInitDB(t)
	t.Cleanup(dbSession.Close)
	common.TestSetupSchema(t, dbSession)

	org := "test-org"
	user := common.TestBuildUser(t, dbSession, "test-starfleet-id", org, []string{authz.ProviderAdminRole})
	ip := common.TestBuildInfrastructureProvider(t, dbSession, "Test Infrastructure Provider", org, user)
	site := common.TestBuildSite(t, dbSession, ip, "Test Site", user)
	it := common.TestBuildInstanceType(t, dbSession, "test-instance-type", cutil.GetPtr(site.ID), site, nil, user)
	machine := common.TestBuildMachine(t, dbSession, ip, site, &it.ID, cutil.GetPtr("test-controller-machine-type"), cdbm.MachineStatusReady)

	proxiedReq := &coreproxy.Request{}
	wrun := &tmocks.WorkflowRun{}
	wrun.On("Get", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		out := args.Get(1).(*coreproxy.Response)
		respJSON, err := protojson.Marshal(&cwssaws.AdminBmcResetResponse{})
		require.NoError(t, err)
		out.ResponseJSON = respJSON
	}).Return(nil)

	tsc := &tmocks.Client{}
	tsc.On(
		"ExecuteWorkflow",
		mock.Anything,
		mock.Anything,
		coreproxy.WorkflowName,
		mock.MatchedBy(func(req coreproxy.Request) bool {
			*proxiedReq = req
			return true
		}),
	).Return(wrun, nil)

	scp := sc.NewClientPool(nil)
	scp.IDClientMap[site.ID.String()] = tsc

	h := NewBmcResetHandler(dbSession, scp, common.GetTestConfig())
	return bmcResetHandlerFixture{
		org:        org,
		siteID:     site.ID.String(),
		machineID:  machine.ID,
		user:       user,
		handler:    h.Handle,
		proxiedReq: proxiedReq,
	}
}

func (f bmcResetHandlerFixture) request(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reqBody string
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = string(bodyBytes)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
	if body != nil {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	ec := e.NewContext(req, rec)
	ec.SetParamNames("orgName", "machineId")
	ec.SetParamValues(f.org, f.machineID)
	ec.Set("user", f.user)

	require.NoError(t, f.handler(ec))
	return rec
}

func TestBmcResetHandlerProxiesRequest(t *testing.T) {
	fixture := newBmcResetHandlerFixture(t)

	rec := fixture.request(t, model.APIBmcResetRequest{UseIpmitool: true})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, cwssaws.Forge_AdminBmcReset_FullMethodName, fixture.proxiedReq.FullMethod)
	assert.Empty(t, fixture.proxiedReq.EncryptedSecrets)

	var coreReq cwssaws.AdminBmcResetRequest
	require.NoError(t, protojson.Unmarshal(fixture.proxiedReq.RequestJSON, &coreReq))
	assert.Equal(t, fixture.machineID, coreReq.GetMachineId())
	assert.True(t, coreReq.GetUseIpmitool())
	assert.NotContains(t, rec.Body.String(), "password")
}

func TestBmcResetHandlerRequiresProviderAdmin(t *testing.T) {
	fixture := newBmcResetHandlerFixture(t)
	fixture.user = &cdbm.User{OrgData: cdbm.OrgData{fixture.org: cdbm.Org{Name: fixture.org}}}

	rec := fixture.request(t, model.APIBmcResetRequest{})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, fixture.proxiedReq.FullMethod)
}

func TestBmcResetHandlerRejectsMissingMachine(t *testing.T) {
	fixture := newBmcResetHandlerFixture(t)
	fixture.machineID = "missing-machine"

	rec := fixture.request(t, model.APIBmcResetRequest{})
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, fixture.proxiedReq.FullMethod)
}
