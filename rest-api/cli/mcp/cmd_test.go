// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegisterHandler_ValidPath(t *testing.T) {
	mux := http.NewServeMux()
	err := registerHandler(mux, "/mcp", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	require.NoError(t, err)
}

func TestRegisterHandler_InvalidPatternReturnsError(t *testing.T) {
	mux := http.NewServeMux()
	err := registerHandler(mux, "/{", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid --path")
}
