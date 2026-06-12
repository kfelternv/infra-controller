// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package server serves the NICo REST read surface as MCP tools over
// streamable-HTTP. Tools are projected 1:1 from the embedded OpenAPI
// spec's GET operations. The server is stateless and never emits SSE:
// every tool/call returns a single application/json body.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	appcli "github.com/NVIDIA/infra-controller/rest-api/cli/pkg"
)

// BuildServer constructs an *mcp.Server with one tool registered for
// every GET operation in the supplied OpenAPI spec. Tool names follow
// the SDD: nico_<snake_case(operationId)>. Each tool handler builds a
// fresh appcli.Client per call from resolveCallConfig and forwards the
// bearer token from the inbound MCP request to NICo REST unchanged.
//
// BuildServer does not start a listener; callers wrap the result with
// NewHandler to get an *http.Handler ready for ListenAndServe.
func BuildServer(specData []byte, opts Options) (*mcp.Server, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(specData)
	if err != nil {
		return nil, fmt.Errorf("parsing spec: %w", err)
	}
	opts = opts.withDefaults()

	version := ""
	if doc.Info != nil {
		version = doc.Info.Version
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "nico-mcp",
		Title:   "NVIDIA Infrastructure Controller (NICo) MCP",
		Version: version,
	}, nil)

	paths := doc.Paths.Map()
	for _, path := range sortedPaths(paths) {
		item := paths[path]
		if item.Get == nil || item.Get.OperationID == "" {
			continue
		}
		registerGET(server, path, item, opts)
	}
	return server, nil
}

// NewHandler wraps an *mcp.Server in a streamable-HTTP handler
// configured for stateless, request/response-only operation:
//
//   - Stateless: true   -- the SDK does not validate Mcp-Session-Id
//     and rejects server->client requests. initialize is a no-op.
//   - JSONResponse: true -- every tool/call response uses
//     Content-Type: application/json; the SDK never opens an SSE
//     stream, so clients and intermediaries that do not speak
//     text/event-stream still receive a single JSON body.
//
// DNS-rebinding (localhost) protection and cross-origin protection are
// deliberately left at the SDK's secure defaults (go-sdk v1.4.1+):
// browser cross-origin requests and localhost DNS-rebinding attempts are
// rejected, while non-browser MCP clients -- which send no Origin
// or Sec-Fetch-Site header -- pass through unaffected. Do not set
// DisableLocalhostProtection or a permissive CrossOriginProtection here
// without understanding the security trade-off.
func NewHandler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
		},
	)
}

// sortedPaths returns the path keys in deterministic order so the
// resulting tool list is stable across server restarts.
func sortedPaths(paths map[string]*openapi3.PathItem) []string {
	return slices.Sorted(maps.Keys(paths))
}

func registerGET(server *mcp.Server, path string, item *openapi3.PathItem, opts Options) {
	op := item.Get
	h := NicoOpenAPIHandler{item: item, op: op}
	allParams := h.mergeParameters()

	tool := &mcp.Tool{
		Name:        toolName(op.OperationID),
		Description: toolDescription(op),
		InputSchema: h.buildInput(),
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
			Title:        op.Summary,
		},
	}

	mcp.AddTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, in map[string]any) (*mcp.CallToolResult, any, error) {
		var cfg resolvedConfig
		if err := cfg.FromCallConfig(in, req, opts); err != nil {
			return errorResult(err), nil, nil
		}
		client := appcli.NewClient(cfg.BaseURL, cfg.Org, cfg.Token, opts.Log, opts.Debug)
		client.APIName = cfg.APIName

		pathParams, queryParams, err := splitArgs(in, allParams)
		if err != nil {
			return errorResult(err), nil, nil
		}
		body, respHeader, err := client.Do(http.MethodGet, path, pathParams, queryParams, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return jsonResult(body, respHeader), nil, nil
	})
}

// toolName converts an operationId to the SDD's canonical MCP tool name
// nico_<snake_case(operationId)>. It handles kebab-case (get-all-site ->
// nico_get_all_site) and camelCase (getFooStatus -> nico_get_foo_status)
// equally: any non-alphanumeric run becomes a single underscore, an
// underscore is inserted at lower/digit -> upper transitions, and the
// result is lowercased and trimmed of leading/trailing underscores.
func toolName(operationID string) string {
	return "nico_" + toSnakeCase(operationID)
}

func toSnakeCase(s string) string {
	var b strings.Builder
	var prev rune
	for i, r := range s {
		switch {
		case unicode.IsUpper(r):
			if i > 0 && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		prev = r
	}
	out := b.String()
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	return strings.Trim(out, "_")
}

func toolDescription(op *openapi3.Operation) string {
	parts := make([]string, 0, 2)
	if s := strings.TrimSpace(op.Summary); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(op.Description); s != "" {
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return op.OperationID
	}
	return strings.Join(parts, "\n\n")
}

// splitArgs maps the tool input map onto path and query parameters
// using the OpenAPI parameter definitions. Common config keys (org,
// base_url, api_name, token) and any unrecognised keys are dropped:
// they are consumed by resolveCallConfig, not the URL builder. The
// "org" path parameter is intentionally skipped here because
// appcli.Client.Do substitutes {org} from Client.Org, which
// resolveCallConfig sets from the per-call override or config layer.
//
// TODO: full OpenAPI style/explode serialization for array and object
// parameters is intentionally deferred; unsupported shapes fail fast.
func splitArgs(in map[string]any, params []*openapi3.Parameter) (pathParams, queryParams map[string]string, err error) {
	pathParams = map[string]string{}
	queryParams = map[string]string{}
	for _, p := range params {
		if p.Name == "org" {
			continue
		}
		raw, ok := in[p.Name]
		if !ok {
			continue
		}
		s, ok := coerceToString(raw)
		if !ok {
			return nil, nil, fmt.Errorf("unsupported argument type for %q: %T", p.Name, raw)
		}
		if s == "" {
			continue
		}
		switch p.In {
		case "path":
			pathParams[p.Name] = s
		case "query":
			queryParams[p.Name] = s
		}
	}
	return pathParams, queryParams, nil
}

func coerceToString(v any) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", true
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case float64:
		// JSON numbers decode to float64; format integers without the
		// decimal point so they round-trip through query strings.
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10), true
		}
		return strconv.FormatFloat(t, 'g', -1, 64), true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case json.Number:
		return t.String(), true
	default:
		return "", false
	}
}

func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

// jsonResult wraps a successful REST response body as a single JSON text
// content block. When the upstream response carries pagination metadata
// (the X-Pagination header NICo REST sets on list endpoints), it is
// surfaced under the result's _meta.pagination so MCP clients can page
// without the metadata polluting the tool's primary JSON payload.
func jsonResult(body []byte, header http.Header) *mcp.CallToolResult {
	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}
	if meta := paginationMeta(header); meta != nil {
		res.Meta = meta
	}
	return res
}

// paginationMeta extracts NICo REST's X-Pagination response header into an
// MCP _meta map. The header value is JSON (e.g.
// {"pageNumber":1,"pageSize":50,"total":1234,"orderBy":null}); it is
// parsed so clients get structured fields, falling back to the raw string
// if it is not valid JSON. Returns nil when the header is absent.
func paginationMeta(header http.Header) mcp.Meta {
	raw := header.Get("X-Pagination")
	if raw == "" {
		return nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		parsed = raw
	}
	return mcp.Meta{"pagination": parsed}
}
