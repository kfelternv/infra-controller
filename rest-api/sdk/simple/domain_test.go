// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package simple

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/NVIDIA/infra-controller/rest-api/sdk/standard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainManagerCRUD(t *testing.T) {
	const domainJSON = `{"id":"domain-1","name":"tenant.example.com","siteId":"site-1","tenantId":"tenant-1","status":"Ready","created":"2026-09-02T12:00:00Z","updated":"2026-09-02T12:00:00Z"}`
	requests := make([]string, 0, 5)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/org/test-org/nico/domain":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "tenant.example.com", body["name"])
			assert.Equal(t, "site-1", body["siteId"])
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, domainJSON)
		case r.Method == http.MethodGet && r.URL.Path == "/v2/org/test-org/nico/domain":
			assert.Equal(t, "site-1", r.URL.Query().Get("siteId"))
			assert.Equal(t, "tenant-1", r.URL.Query().Get("tenantId"))
			w.Header().Set("X-Pagination", `{"pageNumber":1,"pageSize":20,"total":1}`)
			_, _ = io.WriteString(w, "["+domainJSON+"]")
		case r.Method == http.MethodGet && r.URL.Path == "/v2/org/test-org/nico/domain/domain-1":
			_, _ = io.WriteString(w, domainJSON)
		case r.Method == http.MethodDelete && r.URL.Path == "/v2/org/test-org/nico/domain/domain-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newSimpleTestClient(server.URL)
	manager := NewDomainManager(client)
	ctx := context.Background()

	created, apiErr := manager.Create(ctx, DomainCreateRequest{Name: "tenant.example.com"})
	require.Nil(t, apiErr)
	require.NotNil(t, created)
	assert.Equal(t, "domain-1", created.ID)
	assert.Equal(t, "site-1", created.SiteID)
	assert.Equal(t, "tenant-1", created.TenantID)
	assert.Equal(t, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC), created.Created)

	siteID := "site-1"
	tenantID := "tenant-1"
	domains, _, apiErr := manager.GetDomains(ctx, &DomainFilter{SiteID: &siteID, TenantID: &tenantID}, nil)
	require.Nil(t, apiErr)
	require.Len(t, domains, 1)
	assert.Equal(t, "tenant.example.com", domains[0].Name)

	domain, apiErr := manager.Get(ctx, "domain-1")
	require.Nil(t, apiErr)
	require.NotNil(t, domain)
	assert.Equal(t, "domain-1", domain.ID)

	require.Nil(t, manager.Delete(ctx, "domain-1"))
	assert.Equal(t, []string{
		"POST /v2/org/test-org/nico/domain",
		"GET /v2/org/test-org/nico/domain?orderBy=CREATED_ASC&pageNumber=1&siteId=site-1&tenantId=tenant-1",
		"GET /v2/org/test-org/nico/domain/domain-1",
		"DELETE /v2/org/test-org/nico/domain/domain-1",
	}, requests)
}

func TestDomainManager_GetDomains(t *testing.T) {
	site, tenant, order := "site-1", "tenant-1", "NAME_DESC"
	page, size := 2, 1
	for _, tc := range []struct {
		name       string
		filter     *DomainFilter
		pagination *PaginationFilter
		query      url.Values
		header     string
		body       string
		wantPage   *standard.PaginationResponse
		wantIDs    []string
		wantError  int
	}{
		{
			name:     "default first page",
			query:    url.Values{"pageNumber": {"1"}, "orderBy": {"CREATED_ASC"}},
			header:   `{"pageNumber":1,"pageSize":20,"total":2}`,
			body:     "[" + validDomainResponseJSON + "]",
			wantPage: &standard.PaginationResponse{PageNumber: 1, PageSize: 20, Total: 2},
			wantIDs:  []string{"domain-1"},
		},
		{
			name:       "later page retains owner filters and order",
			filter:     &DomainFilter{SiteID: &site, TenantID: &tenant},
			pagination: &PaginationFilter{PageNumber: &page, PageSize: &size, OrderBy: &order},
			query:      url.Values{"pageNumber": {"2"}, "pageSize": {"1"}, "orderBy": {"NAME_DESC"}, "siteId": {site}, "tenantId": {tenant}},
			header:     `{"pageNumber":2,"pageSize":1,"total":2,"orderBy":"NAME_DESC"}`,
			body:       `[{"id":"domain-2","name":"second.example.com","siteId":"site-1","tenantId":"tenant-1","status":"Ready","created":"2026-09-02T12:00:00Z","updated":"2026-09-02T12:00:00Z"}]`,
			wantPage:   &standard.PaginationResponse{PageNumber: 2, PageSize: 1, Total: 2, OrderBy: &order},
			wantIDs:    []string{"domain-2"},
		},
		{
			name:   "empty result retains pagination",
			query:  url.Values{"pageNumber": {"1"}, "orderBy": {"CREATED_ASC"}},
			header: `{"pageNumber":1,"pageSize":20,"total":0}`, body: "[]",
			wantPage: &standard.PaginationResponse{PageNumber: 1, PageSize: 20}, wantIDs: []string{},
		},
		{
			name:   "invalid pagination is not reported as success",
			query:  url.Values{"pageNumber": {"1"}, "orderBy": {"CREATED_ASC"}},
			header: "invalid", body: "[]", wantError: http.StatusInternalServerError,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/v2/org/test-org/nico/domain", r.URL.Path)
				assert.Equal(t, tc.query, r.URL.Query())
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Pagination", tc.header)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			domains, pagination, apiErr := newSimpleTestClient(server.URL).GetDomains(t.Context(), tc.filter, tc.pagination)
			if tc.wantError != 0 {
				require.NotNil(t, apiErr)
				require.Equal(t, tc.wantError, apiErr.Code)
				require.Nil(t, domains)
				require.Nil(t, pagination)
				return
			}
			require.Nil(t, apiErr)
			require.Equal(t, tc.wantPage, pagination)
			require.NotNil(t, domains)
			ids := make([]string, 0, len(domains))
			for _, domain := range domains {
				ids = append(ids, domain.ID)
			}
			require.Equal(t, tc.wantIDs, ids)
		})
	}
}

func TestDomainManager_CreateResponseValidation(t *testing.T) {
	for _, tt := range []struct {
		name       string
		status     int
		body       string
		wantStatus string
		wantError  int
	}{
		{name: "pending reservation", status: http.StatusAccepted, body: `{"id":"domain-1","name":"tenant.example.com","siteId":"site-1","tenantId":"tenant-1","status":"Pending","created":"2026-09-02T12:00:00Z","updated":"2026-09-02T12:00:00Z"}`, wantStatus: "Pending"},
		{name: "missing required status", status: http.StatusCreated, body: `{"id":"domain-1","name":"tenant.example.com","siteId":"site-1","tenantId":"tenant-1","created":"2026-09-02T12:00:00Z","updated":"2026-09-02T12:00:00Z"}`, wantError: http.StatusBadGateway},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v2/org/test-org/nico/domain", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			domain, apiErr := NewDomainManager(newSimpleTestClient(server.URL)).Create(context.Background(), DomainCreateRequest{Name: "tenant.example.com"})
			if tt.wantError != 0 {
				require.Nil(t, domain, "invalid 2xx model must not look like successful create")
				require.NotNil(t, apiErr)
				assert.Equal(t, tt.wantError, apiErr.Code)
				return
			}
			require.Nil(t, apiErr)
			require.NotNil(t, domain)
			assert.Equal(t, "domain-1", domain.ID)
			assert.Equal(t, tt.wantStatus, domain.Status)
		})
	}
}
