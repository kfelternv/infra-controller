// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package simple

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApiMetadata_SetDefaultSubnet(t *testing.T) {
	for _, tc := range []struct {
		name          string
		preset        bool
		presetSite    string
		presetVpc     string
		getStatus     int
		listStatus    int
		emptyList     bool
		emptyID       bool
		wantID        string
		wantName      string
		wantCode      int
		wantListCalls int
	}{
		{name: "matching preset", preset: true, presetSite: "site-1", presetVpc: "vpc-1", wantID: "preset", wantName: "preset-name"},
		{name: "wrong Site falls back to scoped list", preset: true, presetSite: "site-2", presetVpc: "vpc-1", wantID: "first", wantName: "first-name", wantListCalls: 1},
		{name: "wrong VPC falls back to scoped list", preset: true, presetSite: "site-1", presetVpc: "vpc-2", wantID: "first", wantName: "first-name", wantListCalls: 1},
		{name: "empty Site falls back to scoped list", preset: true, presetVpc: "vpc-1", wantID: "first", wantName: "first-name", wantListCalls: 1},
		{name: "empty VPC falls back to scoped list", preset: true, presetSite: "site-1", wantID: "first", wantName: "first-name", wantListCalls: 1},
		{name: "first of multiple available Subnets", wantID: "first", wantName: "first-name", wantListCalls: 1},
		{name: "empty list clears stale name", emptyList: true, wantListCalls: 1},
		{name: "preset not found propagates error", preset: true, getStatus: http.StatusNotFound, wantID: "preset", wantCode: http.StatusNotFound},
		{name: "list error propagates", listStatus: http.StatusServiceUnavailable, wantCode: http.StatusServiceUnavailable, wantListCalls: 1},
		{name: "empty first ID rejected", emptyID: true, wantCode: http.StatusInternalServerError, wantListCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var template map[string]any
			require.NoError(t, json.Unmarshal([]byte(validSubnetResponseJSON), &template))
			getCalls, listCalls := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				assert.Equal(t, http.MethodGet, r.Method)
				switch r.URL.Path {
				case "/v2/org/test-org/nico/subnet/preset":
					getCalls++
					if tc.getStatus != 0 {
						w.WriteHeader(tc.getStatus)
						assert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"message": "lookup failed"}))
						return
					}
					template["id"], template["name"] = "preset", "preset-name"
					template["siteId"], template["vpcId"] = tc.presetSite, tc.presetVpc
					assert.NoError(t, json.NewEncoder(w).Encode(template))
				case "/v2/org/test-org/nico/subnet":
					listCalls++
					assert.Equal(t, "site-1", r.URL.Query().Get("siteId"))
					assert.Equal(t, "vpc-1", r.URL.Query().Get("vpcId"))
					if tc.listStatus != 0 {
						w.WriteHeader(tc.listStatus)
						assert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"message": "list failed"}))
						return
					}
					if tc.emptyList {
						assert.NoError(t, json.NewEncoder(w).Encode([]any{}))
						return
					}
					template["id"], template["name"] = "first", "first-name"
					template["siteId"], template["vpcId"] = "site-1", "vpc-1"
					if tc.emptyID {
						template["id"] = ""
					}
					second := map[string]any{}
					for key, value := range template {
						second[key] = value
					}
					second["id"], second["name"] = "second", "second-name"
					assert.NoError(t, json.NewEncoder(w).Encode([]any{template, second}))
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := newSimpleTestClient(server.URL)
			metadata := ApiMetadata{Organization: "test-org", SiteID: "site-1", VpcID: "vpc-1", SubnetName: "stale-name"}
			if tc.preset {
				metadata.SubnetID = "preset"
			}
			apiErr := metadata.SetDefaultSubnet(WithLogger(context.Background(), client.Logger), client.apiClient)
			if tc.wantCode == 0 {
				require.Nil(t, apiErr)
			} else {
				require.NotNil(t, apiErr)
				assert.Equal(t, tc.wantCode, apiErr.Code)
			}
			assert.Equal(t, tc.wantID, metadata.SubnetID)
			assert.Equal(t, tc.wantName, metadata.SubnetName)
			assert.Equal(t, tc.wantListCalls, listCalls)
			if tc.preset {
				assert.Equal(t, 1, getCalls)
			} else {
				assert.Zero(t, getCalls)
			}
		})
	}
}
