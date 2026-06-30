// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cwssaws "github.com/NVIDIA/infra-controller/rest-api/workflow-schema/schema/site-agent/workflows/v1"
)

func adminOpsStrPtr(s string) *string { return &s }

func TestAPIMachineHealthReportEntryValidateAndToProto(t *testing.T) {
	observedAt := "2026-06-24T12:00:00Z"
	inAlertSince := "2026-06-24T11:00:00Z"
	req := APIMachineHealthReportEntry{
		Source:      "overrides.sre",
		TriggeredBy: adminOpsStrPtr("operator"),
		ObservedAt:  &observedAt,
		Mode:        MachineHealthReportModeReplace,
		Successes: []APIMachineHealthProbeSuccess{
			{ID: "probe.ok", Target: adminOpsStrPtr("host")},
		},
		Alerts: []APIMachineHealthProbeAlert{
			{
				ID:              "probe.alert",
				Target:          adminOpsStrPtr("gpu0"),
				InAlertSince:    &inAlertSince,
				Message:         "forced unhealthy",
				TenantMessage:   adminOpsStrPtr("maintenance"),
				Classifications: []string{"maintenance"},
			},
		},
	}
	require.NoError(t, req.Validate())

	protoReq := req.ToProto("machine-1")
	assert.Equal(t, "machine-1", protoReq.GetMachineId().GetId())
	entry := protoReq.GetHealthReportEntry()
	require.NotNil(t, entry)
	assert.Equal(t, cwssaws.HealthReportApplyMode_Replace, entry.GetMode())
	report := entry.GetReport()
	require.NotNil(t, report)
	assert.Equal(t, "overrides.sre", report.GetSource())
	assert.Equal(t, "operator", report.GetTriggeredBy())
	assert.Equal(t, observedAt, report.GetObservedAt().AsTime().Format("2006-01-02T15:04:05Z07:00"))
	require.Len(t, report.GetSuccesses(), 1)
	assert.Equal(t, "probe.ok", report.GetSuccesses()[0].GetId())
	require.Len(t, report.GetAlerts(), 1)
	assert.Equal(t, "probe.alert", report.GetAlerts()[0].GetId())
	assert.Equal(t, inAlertSince, report.GetAlerts()[0].GetInAlertSince().AsTime().Format("2006-01-02T15:04:05Z07:00"))

	assert.Error(t, (&APIMachineHealthReportEntry{Mode: MachineHealthReportModeMerge}).Validate())
	assert.Error(t, (&APIMachineHealthReportEntry{Source: "source", Mode: "merge"}).Validate())
	assert.Error(t, (&APIMachineHealthReportEntry{Source: "source", Mode: MachineHealthReportModeMerge, ObservedAt: adminOpsStrPtr("bad-time")}).Validate())
	assert.Error(t, (&APIMachineHealthReportEntry{Source: "source", Mode: MachineHealthReportModeMerge, Alerts: []APIMachineHealthProbeAlert{{ID: "alert"}}}).Validate())
}

func TestAPIMachineHealthReportListResponseFromProto(t *testing.T) {
	resp := NewAPIMachineHealthReportListResponse("machine-1", &cwssaws.ListHealthReportResponse{
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

	assert.Equal(t, "machine-1", resp.MachineID)
	require.Len(t, resp.HealthReportEntries, 1)
	assert.Equal(t, MachineHealthReportModeMerge, resp.HealthReportEntries[0].Mode)
	assert.Equal(t, "overrides.sre", resp.HealthReportEntries[0].Source)

	body, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "password")
}
