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
	"google.golang.org/protobuf/proto"

	"github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/handler/util/common"
	"github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/model"
	sc "github.com/NVIDIA/infra-controller/rest-api/api/pkg/client/site"
	authz "github.com/NVIDIA/infra-controller/rest-api/auth/pkg/authorization"
	"github.com/NVIDIA/infra-controller/rest-api/common/pkg/coreproxy"
	cutil "github.com/NVIDIA/infra-controller/rest-api/common/pkg/util"
	cdbm "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db/model"
	cwssaws "github.com/NVIDIA/infra-controller/rest-api/workflow-schema/schema/site-agent/workflows/v1"
)

type adminOpsHandlerFixture struct {
	org        string
	siteID     string
	machineID  string
	user       interface{}
	handler    echo.HandlerFunc
	proxiedReq *coreproxy.Request
}

func newAdminOpsHandlerFixture(t *testing.T, handlerName string, response proto.Message) adminOpsHandlerFixture {
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
		if response == nil {
			return
		}
		out := args.Get(1).(*coreproxy.Response)
		respJSON, err := protojson.Marshal(response)
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

	cfg := common.GetTestConfig()
	var handler echo.HandlerFunc
	switch handlerName {
	case "health-list":
		h := NewListMachineHealthReportHandler(dbSession, scp, cfg)
		handler = h.Handle
	case "health-insert":
		h := NewInsertMachineHealthReportHandler(dbSession, scp, cfg)
		handler = h.Handle
	case "health-remove":
		h := NewRemoveMachineHealthReportHandler(dbSession, scp, cfg)
		handler = h.Handle
	default:
		t.Fatalf("unknown handler %q", handlerName)
	}

	return adminOpsHandlerFixture{
		org:        org,
		siteID:     site.ID.String(),
		machineID:  machine.ID,
		user:       user,
		handler:    handler,
		proxiedReq: proxiedReq,
	}
}

func (f adminOpsHandlerFixture) request(t *testing.T, method, target string, body any, source string) *httptest.ResponseRecorder {
	t.Helper()

	var reqBody string
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = string(bodyBytes)
	}

	e := echo.New()
	req := httptest.NewRequest(method, target, strings.NewReader(reqBody))
	if body != nil {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	ec := e.NewContext(req, rec)
	names := []string{"orgName", "machineId"}
	values := []string{f.org, f.machineID}
	if source != "" {
		names = append(names, "source")
		values = append(values, source)
	}
	ec.SetParamNames(names...)
	ec.SetParamValues(values...)
	ec.Set("user", f.user)

	require.NoError(t, f.handler(ec))
	return rec
}

func TestListMachineHealthReportHandlerProxiesRequest(t *testing.T) {
	fixture := newAdminOpsHandlerFixture(t, "health-list", &cwssaws.ListHealthReportResponse{
		HealthReportEntries: []*cwssaws.HealthReportEntry{
			{
				Mode: cwssaws.HealthReportApplyMode_Merge,
				Report: &cwssaws.HealthReport{
					Source: "overrides.sre",
					Alerts: []*cwssaws.HealthProbeAlert{{Id: "probe.alert", Message: "forced unhealthy"}},
				},
			},
		},
	})

	rec := fixture.request(t, http.MethodGet, "/", nil, "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, cwssaws.Forge_ListMachineHealthReports_FullMethodName, fixture.proxiedReq.FullMethod)
	assert.Empty(t, fixture.proxiedReq.EncryptedSecrets)

	var coreReq cwssaws.MachineId
	require.NoError(t, protojson.Unmarshal(fixture.proxiedReq.RequestJSON, &coreReq))
	assert.Equal(t, fixture.machineID, coreReq.GetId())
	assert.Contains(t, rec.Body.String(), "overrides.sre")
	assert.NotContains(t, rec.Body.String(), "password")
}

func TestInsertMachineHealthReportHandlerProxiesRequest(t *testing.T) {
	fixture := newAdminOpsHandlerFixture(t, "health-insert", nil)
	req := model.APIMachineHealthReportEntry{
		Source: "overrides.sre",
		Mode:   model.MachineHealthReportModeMerge,
		Alerts: []model.APIMachineHealthProbeAlert{{ID: "probe.alert", Message: "forced unhealthy"}},
	}

	rec := fixture.request(t, http.MethodPut, "/", req, "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, cwssaws.Forge_InsertMachineHealthReport_FullMethodName, fixture.proxiedReq.FullMethod)
	assert.Empty(t, fixture.proxiedReq.EncryptedSecrets)

	var coreReq cwssaws.InsertMachineHealthReportRequest
	require.NoError(t, protojson.Unmarshal(fixture.proxiedReq.RequestJSON, &coreReq))
	assert.Equal(t, fixture.machineID, coreReq.GetMachineId().GetId())
	assert.Equal(t, "overrides.sre", coreReq.GetHealthReportEntry().GetReport().GetSource())
	assert.NotContains(t, rec.Body.String(), "password")
}

func TestInsertMachineHealthReportHandlerRejectsInvalidRequest(t *testing.T) {
	fixture := newAdminOpsHandlerFixture(t, "health-insert", nil)

	rec := fixture.request(t, http.MethodPut, "/", model.APIMachineHealthReportEntry{Mode: model.MachineHealthReportModeMerge}, "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, fixture.proxiedReq.FullMethod)
}

func TestRemoveMachineHealthReportHandlerProxiesRequest(t *testing.T) {
	fixture := newAdminOpsHandlerFixture(t, "health-remove", nil)

	rec := fixture.request(t, http.MethodDelete, "/", nil, "overrides.sre")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, cwssaws.Forge_RemoveMachineHealthReport_FullMethodName, fixture.proxiedReq.FullMethod)
	assert.Empty(t, fixture.proxiedReq.EncryptedSecrets)

	var coreReq cwssaws.RemoveMachineHealthReportRequest
	require.NoError(t, protojson.Unmarshal(fixture.proxiedReq.RequestJSON, &coreReq))
	assert.Equal(t, fixture.machineID, coreReq.GetMachineId().GetId())
	assert.Equal(t, "overrides.sre", coreReq.GetSource())
	assert.NotContains(t, rec.Body.String(), "password")
}
