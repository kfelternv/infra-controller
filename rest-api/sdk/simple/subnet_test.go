// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package simple

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToStandardSubnetCreateRequest(t *testing.T) {
	domainID := "domain-1"
	tests := []struct {
		name               string
		subdomainID        *string
		wantSubdomainID    string
		wantHasSubdomainID bool
	}{
		{name: "unset", subdomainID: nil, wantHasSubdomainID: false},
		{name: "populated", subdomainID: &domainID, wantSubdomainID: domainID, wantHasSubdomainID: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiRequest := toStandardSubnetCreateRequest(SubnetCreateRequest{
				Name:         "tenant-net",
				VpcID:        "vpc-1",
				SubdomainID:  test.subdomainID,
				IPv4BlockID:  "block-1",
				PrefixLength: 24,
			})

			assert.Equal(t, test.wantHasSubdomainID, apiRequest.HasSubdomainId())
			assert.Equal(t, test.wantSubdomainID, apiRequest.GetSubdomainId())
			body, err := apiRequest.ToMap()
			require.NoError(t, err)
			if test.wantHasSubdomainID {
				gotSubdomainID, ok := body["subdomainId"].(*string)
				require.True(t, ok)
				require.NotNil(t, gotSubdomainID)
				assert.Equal(t, domainID, *gotSubdomainID)
			} else {
				assert.NotContains(t, body, "subdomainId")
			}
		})
	}
}

func TestClient_CreateSubnet(t *testing.T) {
	requests := make([]string, 0, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		requests = append(requests, r.Method+" "+r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/org/test-org/nico/subnet":
			assert.Equal(t, "tenant-net", body["name"])
			assert.Equal(t, "vpc-1", body["vpcId"])
			assert.Equal(t, "domain-1", body["subdomainId"])
			assert.Equal(t, "block-1", body["ipv4BlockId"])
			assert.Equal(t, float64(24), body["prefixLength"])
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"subnet-1","name":"tenant-net","description":null,"siteId":"site-1","vpcId":"vpc-1","subdomainId":"domain-1","controllerNetworkSegmentId":"controller-subnet-1","ipv4Prefix":"10.0.0.0","ipv4BlockId":"block-1","ipv4Gateway":"10.0.0.1","ipv6Prefix":null,"ipv6BlockId":null,"ipv6Gateway":null,"prefixLength":24,"routingType":"Public","status":"Ready","mtu":null,"statusHistory":[],"created":"2026-09-02T00:00:00Z","updated":"2026-09-02T00:00:00Z"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newSimpleTestClient(server.URL)
	domainID := "domain-1"
	created, apiErr := client.CreateSubnet(context.Background(), SubnetCreateRequest{
		Name:         "tenant-net",
		VpcID:        "vpc-1",
		SubdomainID:  &domainID,
		IPv4BlockID:  "block-1",
		PrefixLength: 24,
	})
	require.Nil(t, apiErr)
	require.NotNil(t, created)
	assert.Equal(t, "subnet-1", created.GetId())

	assert.Equal(t, []string{
		"POST /v2/org/test-org/nico/subnet",
	}, requests)
}
