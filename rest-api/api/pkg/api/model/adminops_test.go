// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIBmcResetRequestToProto(t *testing.T) {
	req := APIBmcResetRequest{UseIpmitool: true}
	require.NoError(t, req.Validate())

	protoReq := req.ToProto("machine-1")
	assert.Equal(t, "machine-1", protoReq.GetMachineId())
	assert.True(t, protoReq.GetUseIpmitool())
}

func TestAPIBmcResetResponseOmitsSecrets(t *testing.T) {
	resp := NewAPIBmcResetResponse("machine-1", &APIBmcResetRequest{UseIpmitool: true})

	body, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(body), "machine-1")
	assert.Contains(t, string(body), "useIpmitool")
	assert.NotContains(t, string(body), "password")
}
