// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
)

// Options carry the server-side defaults that every tool invocation
// starts from. Individual tool calls override these per request through
// resolveCallConfig.
type Options struct {
	// BaseURL is the NICo REST base URL (e.g. https://nico.example.com).
	BaseURL string
	// Org is the default organisation used in /v2/org/<org>/... paths.
	Org string
	// APIName is the API path segment between org and resource (default
	// "nico", overridable via api.name in config).
	APIName string
	// Token is the static bearer used when no inbound bearer or tool
	// arg token is provided.
	Token string
	// Debug enables logrus debug-level HTTP request/response logging
	// through to the appcli.Client.
	Debug bool
	// Log is the logrus entry used for client request/response logging.
	// If nil, a default entry wrapping the standard logger is used.
	Log *logrus.Entry
}

// withDefaults returns a copy of opts with empty optional fields filled
// in with package defaults. APIName falls back to "nico" and Log to
// logrus.StandardLogger() so callers can leave them unset.
func (o Options) withDefaults() Options {
	if o.APIName == "" {
		o.APIName = "nico"
	}
	if o.Log == nil {
		o.Log = logrus.NewEntry(logrus.StandardLogger())
	}
	return o
}

// resolvedConfig is the result of merging Options with the per-call
// overrides for one tool invocation. It is consumed by registerGET to
// construct a fresh appcli.Client.
type resolvedConfig struct {
	BaseURL string
	Org     string
	APIName string
	Token   string
}

// resolveCallConfig implements the precedence chain documented in the
// design plan:
//
//  1. Tool-call argument (org, base_url, api_name, token)
//  2. Inbound Authorization header (token only)
//  3. Server startup flag / Options (BaseURL, Org, APIName, Token)
//
// The function returns an error when a required field (org, base_url)
// ends up empty so the tool handler can surface a JSON-RPC error
// instead of letting the call go out with an invalid URL.
func resolveCallConfig(in map[string]any, req *mcp.CallToolRequest, opts Options) (resolvedConfig, error) {
	cfg := resolvedConfig{
		BaseURL: normalizeBaseURL(pickString(stringArg(in, "base_url"), opts.BaseURL)),
		Org:     pickString(stringArg(in, "org"), opts.Org),
		APIName: pickString(stringArg(in, "api_name"), opts.APIName),
	}

	cfg.Token = normalizeToken(pickString(
		stringArg(in, "token"),
		bearerFromExtra(req),
		opts.Token,
	))

	if err := cfg.requireNonEmpty(); err != nil {
		return resolvedConfig{}, err
	}
	return cfg, nil
}

// requireNonEmpty returns a descriptive error when org or BaseURL are
// blank. Token can be empty -- NICo REST will reject the request with
// 401 and the response surfaces to the caller as an MCP error result;
// that path is exercised by the bearer-passthrough integration test.
func (c resolvedConfig) requireNonEmpty() error {
	missing := []string{}
	if c.Org == "" {
		missing = append(missing, "org")
	}
	if c.BaseURL == "" {
		missing = append(missing, "base_url")
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing required config value(s): %s; pass via tool-call arguments, server flags, or NICO_* environment variables",
		strings.Join(missing, ", "))
}

func stringArg(in map[string]any, key string) string {
	if in == nil {
		return ""
	}
	v, ok := in[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func normalizeBaseURL(v string) string {
	return strings.TrimRight(v, "/")
}

func normalizeToken(v string) string {
	const prefix = "Bearer "
	if len(v) > len(prefix) && strings.EqualFold(v[:len(prefix)], prefix) {
		return strings.TrimSpace(v[len(prefix):])
	}
	return v
}

func pickString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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
