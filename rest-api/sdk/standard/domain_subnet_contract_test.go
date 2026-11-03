// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package standard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const domainResponse = `{
	"id":"domain-id",
	"name":"tenant.example.com",
	"siteId":"site-id",
	"tenantId":"tenant-id",
	"status":"Ready",
	"created":"2026-09-02T12:00:00Z",
	"updated":"2026-09-02T12:00:00Z"
}`

const subnetResponse = `{
	"id":"subnet-id",
	"name":"tenant-subnet",
	"description":null,
	"siteId":"site-id",
	"vpcId":"target-vpc-id",
	"subdomainId":null,
	"controllerNetworkSegmentId":null,
	"ipv4Prefix":null,
	"ipv4BlockId":null,
	"ipv4Gateway":null,
	"ipv6Prefix":null,
	"ipv6BlockId":null,
	"ipv6Gateway":null,
	"prefixLength":24,
	"routingType":null,
	"status":"Ready",
	"statusHistory":[],
	"created":"2026-09-02T12:00:00Z",
	"updated":"2026-09-02T12:00:00Z"
}`

func TestDomainAPIService_WireContract(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		query        url.Values
		body         map[string]any
		status       int
		responseBody string
		execute      func(*APIClient) error
	}{
		{
			name:         "create",
			method:       http.MethodPost,
			path:         "/v2/org/tenant-org/nico/domain",
			query:        url.Values{},
			body:         map[string]any{"name": "tenant.example.com", "siteId": "site-id"},
			status:       http.StatusCreated,
			responseBody: domainResponse,
			execute: func(client *APIClient) error {
				_, _, err := client.DNSDomainAPI.CreateDomain(context.Background(), "tenant-org").
					DomainCreateRequest(*NewDomainCreateRequest("tenant.example.com", "site-id")).
					Execute()
				return err
			},
		},
		{
			name:   "list",
			method: http.MethodGet,
			path:   "/v2/org/tenant-org/nico/domain",
			query: url.Values{
				"siteId":     []string{"site-id"},
				"tenantId":   []string{"tenant-id"},
				"pageNumber": []string{"3"},
				"pageSize":   []string{"25"},
				"orderBy":    []string{"NAME_DESC"},
			},
			status:       http.StatusOK,
			responseBody: "[" + domainResponse + "]",
			execute: func(client *APIClient) error {
				_, _, err := client.DNSDomainAPI.GetAllDomain(context.Background(), "tenant-org").
					SiteId("site-id").
					TenantId("tenant-id").
					PageNumber(3).
					PageSize(25).
					OrderBy("NAME_DESC").
					Execute()
				return err
			},
		},
		{
			name:         "get",
			method:       http.MethodGet,
			path:         "/v2/org/tenant-org/nico/domain/domain-id",
			query:        url.Values{},
			status:       http.StatusOK,
			responseBody: domainResponse,
			execute: func(client *APIClient) error {
				_, _, err := client.DNSDomainAPI.GetDomain(context.Background(), "tenant-org", "domain-id").Execute()
				return err
			},
		},
		{
			name:         "delete",
			method:       http.MethodDelete,
			path:         "/v2/org/tenant-org/nico/domain/domain-id",
			query:        url.Values{},
			status:       http.StatusNoContent,
			responseBody: "",
			execute: func(client *APIClient) error {
				_, err := client.DNSDomainAPI.DeleteDomain(context.Background(), "tenant-org", "domain-id").Execute()
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &sdkContractTransport{status: tt.status, responseBody: tt.responseBody}
			client := newSDKContractClient(transport)

			require.NoError(t, tt.execute(client))
			require.NotNil(t, transport.request)
			require.Equal(t, tt.method, transport.request.Method)
			require.Equal(t, tt.path, transport.request.URL.Path)
			require.Equal(t, tt.query, transport.request.URL.Query())
			if tt.body == nil {
				require.Empty(t, transport.body)
				return
			}

			var body map[string]any
			require.NoError(t, json.Unmarshal(transport.body, &body))
			require.Equal(t, tt.body, body)
		})
	}
}

func TestDomain_MarshalJSON(t *testing.T) {
	encoded, err := json.Marshal(Domain{})
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, json.Unmarshal(encoded, &response))
	require.Len(t, response, 7)
	for _, field := range []string{"id", "name", "siteId", "tenantId", "status", "created", "updated"} {
		require.Contains(t, response, field)
	}
	require.NotContains(t, response, "controllerDomainId")
}

func TestDomainAPIService_DocumentedErrorModels(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
		execute func(*APIClient) error
	}{
		{
			name:    "get domain malformed ID",
			status:  http.StatusBadRequest,
			message: "Invalid Domain ID in URL",
			execute: func(client *APIClient) error {
				_, _, err := client.DNSDomainAPI.GetDomain(context.Background(), "tenant-org", "not-a-uuid").Execute()
				return err
			},
		},
		{
			name:    "delete domain malformed ID",
			status:  http.StatusBadRequest,
			message: "Invalid Domain ID in URL",
			execute: func(client *APIClient) error {
				_, err := client.DNSDomainAPI.DeleteDomain(context.Background(), "tenant-org", "not-a-uuid").Execute()
				return err
			},
		},
		{
			name:    "delete domain not deletable",
			status:  http.StatusConflict,
			message: "Domain is not available for deletion",
			execute: func(client *APIClient) error {
				_, err := client.DNSDomainAPI.DeleteDomain(context.Background(), "tenant-org", "domain-id").Execute()
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &sdkContractTransport{
				status:       tt.status,
				responseBody: `{"source":"nico","message":"` + tt.message + `","data":null}`,
			}
			err := tt.execute(newSDKContractClient(transport))

			var apiErr *GenericOpenAPIError
			require.ErrorAs(t, err, &apiErr)
			model, ok := apiErr.Model().(NICoAPIError)
			require.True(t, ok, "status %d must decode a typed NICoAPIError", tt.status)
			require.Equal(t, tt.message, model.GetMessage())
		})
	}
}

type sdkContractTransport struct {
	request      *http.Request
	body         []byte
	status       int
	responseBody string
}

func (t *sdkContractTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.request = request.Clone(request.Context())
	if request.Body != nil {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		t.body = body
	}

	return &http.Response{
		StatusCode: t.status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(t.responseBody)),
		Request:    request,
	}, nil
}

func newSDKContractClient(transport *sdkContractTransport) *APIClient {
	configuration := NewConfiguration()
	configuration.Servers = ServerConfigurations{{URL: "https://example.com", Description: "test"}}
	configuration.HTTPClient = &http.Client{Transport: transport}
	return NewAPIClient(configuration)
}
