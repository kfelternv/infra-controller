// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appcli "github.com/NVIDIA/infra-controller/rest-api/cli/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testGetAllSuggestions_SubnetAttachUsesSiteWideDiscovery(t *testing.T) {
	for _, command := range []string{"subnet attach-vpc", "subnet attach-vpc-to attach-vpc-to-subnet"} {
		t.Run(command, func(t *testing.T) {
			queries := make(chan string, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v2/org/acme/nico/vpc":
					assert.Equal(t, "site-1", r.URL.Query().Get("siteId"))
					assert.Equal(t, "tenant-1", r.URL.Query().Get("tenantId"))
					_, _ = io.WriteString(w, `[{"id":"vpc-narrow","siteId":"site-1","tenantId":"tenant-1","status":"Ready"},{"id":"vpc-other","siteId":"site-1","tenantId":"tenant-1","status":"Ready"}]`)
				case "/v2/org/acme/nico/subnet":
					queries <- r.URL.Query().Get("vpcId")
					assert.Equal(t, "site-1", r.URL.Query().Get("siteId"))
					assert.Equal(t, "true", r.URL.Query().Get("includeUsageStats"))
					if r.URL.Query().Get("vpcId") == "vpc-narrow" {
						_, _ = io.WriteString(w, `[{"id":"subnet-narrow","name":"narrow-net","siteId":"site-1","vpcId":"vpc-narrow","status":"Ready","usageStats":{"acquiredIPs":2}}]`)
						return
					}
					_, _ = io.WriteString(w, `[{"id":"subnet-other","name":"outside-net","siteId":"site-1","vpcId":"vpc-other","status":"Ready","usageStats":{"acquiredIPs":2}}]`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			s := NewSession(appcli.NewClient(server.URL, "acme", "token", nil, false), "acme", "")
			s.Scope = Scope{SiteID: "site-1", SiteName: "Site One", VpcID: "vpc-narrow", VpcName: "Narrow"}
			original := s.Scope
			s.Cache.Set("_tenant", []NamedItem{{Name: "acme", ID: "tenant-1"}})
			// A prior ordinary Subnet command populated the cache with its VPC scope.
			narrow, err := s.Resolver.Fetch(context.Background(), "subnet")
			require.NoError(t, err)
			require.Len(t, narrow, 1)
			assert.Equal(t, "vpc-narrow", <-queries)
			suggestions := getAllSuggestions(s, command+" outside", []string{command})
			assert.Equal(t, []string{command + " outside-net"}, suggestions)
			assert.Equal(t, original, s.Scope)
			select {
			case query := <-queries:
				assert.Empty(t, query, "attachment discovery is Site-wide")
			default:
				t.Error("attachment reused the narrower cached Subnet list")
			}
			// The Site-wide autocomplete result must not leak into ordinary scoped reads.
			narrowedAgain, err := s.Resolver.Fetch(context.Background(), "subnet")
			require.NoError(t, err)
			require.Len(t, narrowedAgain, 1)
			assert.Equal(t, "subnet-narrow", narrowedAgain[0].ID)
			select {
			case query := <-queries:
				assert.Equal(t, "vpc-narrow", query)
			default:
				t.Error("the scoped fetch reused attachment discovery cache")
			}
		})
	}
}

func testGetAllSuggestions_SubnetAttachCancelsHTTP(t *testing.T) {
	for _, resource := range []string{"tenant/current", "vpc", "subnet"} {
		t.Run(resource, func(t *testing.T) {
			canceled := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v2/org/acme/nico/"+resource {
					// Bound the control even if the client incorrectly ignores its deadline.
					timer := time.NewTimer(2 * autocompleteFetchTimeout)
					defer timer.Stop()
					select {
					case <-r.Context().Done():
						canceled <- struct{}{}
						return
					case <-timer.C:
					}
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v2/org/acme/nico/tenant/current":
					_, _ = io.WriteString(w, `{"id":"tenant-1"}`)
				case "/v2/org/acme/nico/vpc":
					_, _ = io.WriteString(w, `[{"id":"vpc-1","siteId":"site-1","tenantId":"tenant-1","status":"Ready"}]`)
				case "/v2/org/acme/nico/subnet":
					_, _ = io.WriteString(w, `[{"id":"subnet-1","name":"eligible-net","siteId":"site-1","vpcId":"vpc-1","status":"Ready","usageStats":{"acquiredIPs":2}}]`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			s := NewSession(appcli.NewClient(server.URL, "acme", "token", nil, false), "acme", "")
			s.Scope.SiteID = "site-1"
			started := time.Now()
			suggestions := getAllSuggestions(s, "subnet attach-vpc eligible", []string{"subnet attach-vpc"})
			elapsed := time.Since(started)
			assert.Nil(t, suggestions)
			assert.Less(t, elapsed, autocompleteFetchTimeout+time.Second)
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Error("deadline did not cancel the actual HTTP request")
			}
		})
	}
}

func testGetAllSuggestions_CompletionDoesNotRefreshAuth(t *testing.T) {
	for _, command := range []string{"subnet attach-vpc", "dns-domain get"} {
		t.Run(command, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
			defer server.Close()
			client := appcli.NewClient(server.URL, "acme", "token", nil, false)
			refreshes := 0
			client.TokenRefresh = func() (string, error) { refreshes++; return "refreshed-token", nil }
			s := NewSession(client, "acme", "")
			s.Scope = Scope{SiteID: "site-1", VpcID: "vpc-narrow", VpcName: "Narrow"}
			originalScope := s.Scope
			assert.Nil(t, getAllSuggestions(s, command+" resource", []string{command}))
			assert.Zero(t, refreshes, "typing must not start a configured interactive login")
			assert.Equal(t, "token", client.Token)
			assert.Same(t, client, s.Client)
			assert.Equal(t, originalScope, s.Scope)
			// Completion must not disable normal command authentication recovery.
			_, _, err := s.Client.Do(http.MethodGet, "/v2/org/acme/nico/tenant/current", nil, nil, nil)
			require.Error(t, err)
			assert.Equal(t, 3, refreshes)
			assert.Equal(t, "refreshed-token", client.Token)
		})
	}
}
