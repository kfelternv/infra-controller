// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stringArg returns the trimmed string value for key in a tool-call
// argument map, or "" when the key is absent or not a string. Reading a
// nil map or a missing key yields the zero value, and the comma-ok type
// assertion yields "" for any non-string value.
func stringArg(in map[string]any, key string) string {
	s, _ := in[key].(string)
	return strings.TrimSpace(s)
}

// normalizeBaseURL trims trailing slashes so joining the base with an
// absolute "/v2/..." path never yields a double slash.
func normalizeBaseURL(v string) string {
	return strings.TrimRight(v, "/")
}

// normalizeToken strips a leading "Bearer " scheme (case-insensitive) so
// callers can pass either a raw token or a full Authorization value.
func normalizeToken(v string) string {
	const prefix = "Bearer "
	if len(v) > len(prefix) && strings.EqualFold(v[:len(prefix)], prefix) {
		return strings.TrimSpace(v[len(prefix):])
	}
	return v
}

// bearerFromExtra extracts the bearer from a *mcp.CallToolRequest's
// inbound HTTP headers. The streamable-HTTP handler stamps every JSON-RPC
// request with req.Extra.Header from the HTTP request. Returns the bare
// token without the "Bearer " prefix; returns "" for any value the SDK
// did not stash or that does not look like a bearer.
func bearerFromExtra(req *mcp.CallToolRequest) string {
	if req == nil || req.Extra == nil || req.Extra.Header == nil {
		return ""
	}
	auth := req.Extra.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(auth[len(prefix):])
}
