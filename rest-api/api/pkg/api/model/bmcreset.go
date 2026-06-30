// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model

import cwssaws "github.com/NVIDIA/infra-controller/rest-api/workflow-schema/schema/site-agent/workflows/v1"

type APIBmcResetRequest struct {
	UseIpmitool bool `json:"useIpmitool,omitempty"`
}

func (r *APIBmcResetRequest) Validate() error {
	return nil
}

func (r *APIBmcResetRequest) ToProto(machineID string) *cwssaws.AdminBmcResetRequest {
	return &cwssaws.AdminBmcResetRequest{
		MachineId:   &machineID,
		UseIpmitool: r.UseIpmitool,
	}
}

type APIBmcResetResponse struct {
	MachineID   string `json:"machineId"`
	UseIpmitool bool   `json:"useIpmitool,omitempty"`
}

func NewAPIBmcResetResponse(machineID string, req *APIBmcResetRequest) *APIBmcResetResponse {
	return &APIBmcResetResponse{
		MachineID:   machineID,
		UseIpmitool: req.UseIpmitool,
	}
}
