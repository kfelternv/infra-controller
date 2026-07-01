// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"fmt"
	"time"

	cwssaws "github.com/NVIDIA/infra-controller/rest-api/workflow-schema/schema/site-agent/workflows/v1"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	MachineHealthReportModeMerge   = "Merge"
	MachineHealthReportModeReplace = "Replace"
)

var validMachineHealthReportModes = []string{
	MachineHealthReportModeMerge,
	MachineHealthReportModeReplace,
}

var validMachineHealthReportModesAny = func() []interface{} {
	result := make([]interface{}, len(validMachineHealthReportModes))
	for i, mode := range validMachineHealthReportModes {
		result[i] = mode
	}
	return result
}()

type APIMachineHealthReportEntry struct {
	Source      string                         `json:"source"`
	TriggeredBy *string                        `json:"triggeredBy,omitempty"`
	ObservedAt  *string                        `json:"observedAt,omitempty"`
	Successes   []APIMachineHealthProbeSuccess `json:"successes,omitempty"`
	Alerts      []APIMachineHealthProbeAlert   `json:"alerts,omitempty"`
	Mode        string                         `json:"mode"`
}

func (r *APIMachineHealthReportEntry) Validate() error {
	if err := validation.ValidateStruct(r,
		validation.Field(&r.Source, validation.Required.Error(validationErrorValueRequired)),
		validation.Field(&r.Mode,
			validation.Required.Error(validationErrorValueRequired),
			validation.In(validMachineHealthReportModesAny...).Error(fmt.Sprintf("must be one of %v", validMachineHealthReportModes))),
	); err != nil {
		return err
	}
	if err := validateOptionalRFC3339(r.ObservedAt, "observedAt"); err != nil {
		return err
	}
	for i := range r.Successes {
		if err := validation.ValidateStruct(&r.Successes[i],
			validation.Field(&r.Successes[i].ID, validation.Required.Error(validationErrorValueRequired)),
		); err != nil {
			return fmt.Errorf("successes[%d]: %w", i, err)
		}
	}
	for i := range r.Alerts {
		if err := validateOptionalRFC3339(r.Alerts[i].InAlertSince, fmt.Sprintf("alerts[%d].inAlertSince", i)); err != nil {
			return err
		}
		err := validation.ValidateStruct(&r.Alerts[i],
			validation.Field(&r.Alerts[i].ID, validation.Required.Error(validationErrorValueRequired)),
			validation.Field(&r.Alerts[i].Message, validation.Required.Error(validationErrorValueRequired)),
		)
		if err != nil {
			return fmt.Errorf("alerts[%d]: %w", i, err)
		}
	}
	return nil
}

func (r *APIMachineHealthReportEntry) ToProto(machineID string) *cwssaws.InsertMachineHealthReportRequest {
	return &cwssaws.InsertMachineHealthReportRequest{
		MachineId: &cwssaws.MachineId{Id: machineID},
		HealthReportEntry: &cwssaws.HealthReportEntry{
			Report: &cwssaws.HealthReport{
				Source:      r.Source,
				TriggeredBy: r.TriggeredBy,
				ObservedAt:  stringToProtoTime(r.ObservedAt),
				Successes:   successesToProto(r.Successes),
				Alerts:      alertsToProto(r.Alerts),
			},
			Mode: healthReportModeToProto(r.Mode),
		},
	}
}

func NewAPIMachineHealthReportEntries(resp *cwssaws.ListHealthReportResponse) []APIMachineHealthReportEntry {
	entries := []APIMachineHealthReportEntry{}
	if resp != nil {
		entries = make([]APIMachineHealthReportEntry, 0, len(resp.GetHealthReportEntries()))
		for _, entry := range resp.GetHealthReportEntries() {
			entries = append(entries, machineHealthReportEntryFromProto(entry))
		}
	}
	return entries
}

func NewMachineIDProto(machineID string) *cwssaws.MachineId {
	return &cwssaws.MachineId{Id: machineID}
}

func NewRemoveMachineHealthReportProto(machineID, source string) *cwssaws.RemoveMachineHealthReportRequest {
	return &cwssaws.RemoveMachineHealthReportRequest{
		MachineId: NewMachineIDProto(machineID),
		Source:    source,
	}
}

func healthReportModeToProto(mode string) cwssaws.HealthReportApplyMode {
	switch mode {
	case MachineHealthReportModeReplace:
		return cwssaws.HealthReportApplyMode_Replace
	default:
		return cwssaws.HealthReportApplyMode_Merge
	}
}

func machineHealthReportModeFromProto(mode cwssaws.HealthReportApplyMode) string {
	switch mode {
	case cwssaws.HealthReportApplyMode_Replace:
		return MachineHealthReportModeReplace
	default:
		return MachineHealthReportModeMerge
	}
}

func successesToProto(successes []APIMachineHealthProbeSuccess) []*cwssaws.HealthProbeSuccess {
	out := make([]*cwssaws.HealthProbeSuccess, 0, len(successes))
	for _, success := range successes {
		out = append(out, &cwssaws.HealthProbeSuccess{
			Id:     success.ID,
			Target: success.Target,
		})
	}
	return out
}

func alertsToProto(alerts []APIMachineHealthProbeAlert) []*cwssaws.HealthProbeAlert {
	out := make([]*cwssaws.HealthProbeAlert, 0, len(alerts))
	for _, alert := range alerts {
		out = append(out, &cwssaws.HealthProbeAlert{
			Id:              alert.ID,
			Target:          alert.Target,
			InAlertSince:    stringToProtoTime(alert.InAlertSince),
			Message:         alert.Message,
			TenantMessage:   alert.TenantMessage,
			Classifications: alert.Classifications,
		})
	}
	return out
}

func machineHealthReportEntryFromProto(entry *cwssaws.HealthReportEntry) APIMachineHealthReportEntry {
	apiEntry := APIMachineHealthReportEntry{
		Mode: machineHealthReportModeFromProto(entry.GetMode()),
	}
	report := entry.GetReport()
	if report == nil {
		return apiEntry
	}
	apiEntry.Source = report.GetSource()
	apiEntry.TriggeredBy = report.TriggeredBy
	apiEntry.ObservedAt = stringFromProtoTime(report.GetObservedAt())
	apiEntry.Successes = successesFromProto(report.GetSuccesses())
	apiEntry.Alerts = alertsFromProto(report.GetAlerts())
	return apiEntry
}

func successesFromProto(successes []*cwssaws.HealthProbeSuccess) []APIMachineHealthProbeSuccess {
	out := make([]APIMachineHealthProbeSuccess, 0, len(successes))
	for _, success := range successes {
		out = append(out, APIMachineHealthProbeSuccess{
			ID:     success.GetId(),
			Target: success.Target,
		})
	}
	return out
}

func alertsFromProto(alerts []*cwssaws.HealthProbeAlert) []APIMachineHealthProbeAlert {
	out := make([]APIMachineHealthProbeAlert, 0, len(alerts))
	for _, alert := range alerts {
		out = append(out, APIMachineHealthProbeAlert{
			ID:              alert.GetId(),
			Target:          alert.Target,
			InAlertSince:    stringFromProtoTime(alert.GetInAlertSince()),
			Message:         alert.GetMessage(),
			TenantMessage:   alert.TenantMessage,
			Classifications: alert.GetClassifications(),
		})
	}
	return out
}

func validateOptionalRFC3339(value *string, field string) error {
	if value == nil {
		return nil
	}
	if *value == "" {
		return fmt.Errorf("%s must be RFC3339 timestamp", field)
	}
	if _, err := time.Parse(time.RFC3339Nano, *value); err != nil {
		return fmt.Errorf("%s must be RFC3339 timestamp: %w", field, err)
	}
	return nil
}

func stringToProtoTime(value *string) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, *value)
	if err != nil {
		return nil
	}
	return timestamppb.New(t)
}

func stringFromProtoTime(t *timestamppb.Timestamp) *string {
	if t == nil {
		return nil
	}
	out := t.AsTime().Format(time.RFC3339Nano)
	return &out
}
