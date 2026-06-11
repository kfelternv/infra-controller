// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestResolveCallConfig_PrecedenceChain(t *testing.T) {
	type expect struct {
		baseURL, org, apiName, token string
		wantErr                      bool
	}
	cases := []struct {
		name     string
		in       map[string]any
		req      *mcp.CallToolRequest
		opts     Options
		expected expect
	}{
		{
			name: "tool_args_win_every_field",
			in: map[string]any{
				"base_url": "https://from-arg.example.com/",
				"org":      "arg-org",
				"api_name": "arg-name",
				"token":    "Bearer arg-token",
			},
			req:  requestWithBearer("inbound-bearer"),
			opts: Options{BaseURL: "https://opts.example.com", Org: "opts-org", APIName: "opts-name", Token: "opts-token"},
			expected: expect{
				baseURL: "https://from-arg.example.com",
				org:     "arg-org",
				apiName: "arg-name",
				token:   "arg-token",
			},
		},
		{
			name: "inbound_bearer_wins_when_no_token_arg",
			in:   map[string]any{},
			req:  requestWithBearer("from-header"),
			opts: Options{BaseURL: "https://opts.example.com", Org: "opts-org", APIName: "nico", Token: "opts-token"},
			expected: expect{
				baseURL: "https://opts.example.com",
				org:     "opts-org",
				apiName: "nico",
				token:   "from-header",
			},
		},
		{
			name: "opts_token_used_when_no_arg_and_no_inbound",
			in:   map[string]any{},
			req:  nil,
			opts: Options{BaseURL: "https://opts.example.com", Org: "opts-org", APIName: "nico", Token: "opts-token"},
			expected: expect{
				baseURL: "https://opts.example.com",
				org:     "opts-org",
				apiName: "nico",
				token:   "opts-token",
			},
		},
		{
			name: "missing_org_errors",
			in:   map[string]any{},
			req:  nil,
			opts: Options{BaseURL: "https://opts.example.com"},
			expected: expect{
				wantErr: true,
			},
		},
		{
			name: "missing_base_url_errors",
			in:   map[string]any{},
			req:  nil,
			opts: Options{Org: "opts-org"},
			expected: expect{
				wantErr: true,
			},
		},
		{
			name: "missing_both_errors",
			in:   map[string]any{},
			req:  nil,
			opts: Options{},
			expected: expect{
				wantErr: true,
			},
		},
		{
			name: "api_name_defaults_when_unset",
			in:   map[string]any{},
			req:  nil,
			opts: Options{BaseURL: "https://opts.example.com", Org: "opts-org", Token: "t"}.withDefaults(),
			expected: expect{
				baseURL: "https://opts.example.com",
				org:     "opts-org",
				apiName: "nico",
				token:   "t",
			},
		},
		{
			name: "empty_string_arg_falls_through_to_opts",
			in: map[string]any{
				"org":   "",
				"token": "   ",
			},
			req: requestWithBearer("inbound"),
			opts: Options{
				BaseURL: "https://opts.example.com",
				Org:     "opts-org",
				APIName: "nico",
				Token:   "opts-token",
			},
			expected: expect{
				baseURL: "https://opts.example.com",
				org:     "opts-org",
				apiName: "nico",
				token:   "inbound",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := resolveCallConfig(c.in, c.req, c.opts)
			if c.expected.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.expected.baseURL, cfg.BaseURL)
			require.Equal(t, c.expected.org, cfg.Org)
			require.Equal(t, c.expected.apiName, cfg.APIName)
			require.Equal(t, c.expected.token, cfg.Token)
		})
	}
}

func TestStringArg(t *testing.T) {
	in := map[string]any{
		"a": "hello",
		"b": "  hello  ",
		"c": 42,
		"d": nil,
		"e": "",
	}
	require.Equal(t, "hello", stringArg(in, "a"))
	require.Equal(t, "hello", stringArg(in, "b"))
	require.Equal(t, "", stringArg(in, "c"))
	require.Equal(t, "", stringArg(in, "d"))
	require.Equal(t, "", stringArg(in, "e"))
	require.Equal(t, "", stringArg(in, "missing"))
	require.Equal(t, "", stringArg(nil, "any"))
}

func TestPickString(t *testing.T) {
	require.Equal(t, "a", pickString("a", "b", "c"))
	require.Equal(t, "b", pickString("", "b", "c"))
	require.Equal(t, "c", pickString("", "", "c"))
	require.Equal(t, "", pickString("", "", ""))
	require.Equal(t, "", pickString())
}

func TestNormalizeBaseURL(t *testing.T) {
	require.Equal(t, "https://api.example.com", normalizeBaseURL("https://api.example.com/"))
	require.Equal(t, "https://api.example.com", normalizeBaseURL("https://api.example.com///"))
	require.Equal(t, "https://api.example.com/v2", normalizeBaseURL("https://api.example.com/v2/"))
	require.Equal(t, "", normalizeBaseURL(""))
}

func TestNormalizeToken(t *testing.T) {
	require.Equal(t, "abc.def", normalizeToken("Bearer abc.def"))
	require.Equal(t, "abc.def", normalizeToken("bearer abc.def"))
	require.Equal(t, "abc.def", normalizeToken("Bearer   abc.def   "))
	require.Equal(t, "abc.def", normalizeToken("abc.def"))
}

func TestBearerFromExtra(t *testing.T) {
	cases := []struct {
		name string
		hdr  http.Header
		want string
	}{
		{"nil_req", nil, ""},
		{"empty_header", http.Header{}, ""},
		{"bearer", http.Header{"Authorization": []string{"Bearer abc.def"}}, "abc.def"},
		{"bearer_lowercase_scheme", http.Header{"Authorization": []string{"bearer abc.def"}}, "abc.def"},
		{"bearer_with_padding", http.Header{"Authorization": []string{"Bearer   spaced   "}}, "spaced"},
		{"non_bearer_basic", http.Header{"Authorization": []string{"Basic dXNlcjpwYXNz"}}, ""},
		{"empty_value", http.Header{"Authorization": []string{""}}, ""},
		{"bearer_alone", http.Header{"Authorization": []string{"Bearer "}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var req *mcp.CallToolRequest
			if c.hdr != nil {
				req = &mcp.CallToolRequest{Extra: &mcp.RequestExtra{Header: c.hdr}}
			}
			require.Equal(t, c.want, bearerFromExtra(req))
		})
	}
}

// TestOptions_WithDefaults_PreservesCustomLogger asserts that
// withDefaults does not clobber a caller-supplied Log entry.
func TestOptions_WithDefaults(t *testing.T) {
	o := Options{}.withDefaults()
	require.Equal(t, "nico", o.APIName)
	require.NotNil(t, o.Log)
}

func requestWithBearer(token string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{
		Extra: &mcp.RequestExtra{
			Header: http.Header{"Authorization": []string{"Bearer " + token}},
		},
	}
}
