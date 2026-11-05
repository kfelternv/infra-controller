// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_DoWithContext(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check func(*testing.T)
	}{
		{"already canceled request sends nothing", func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
			defer server.Close()
			client := NewClient(server.URL, "org", "token", nil, false)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, _, err := client.DoWithContext(ctx, http.MethodGet, "/v2/org/{org}/nico/vpc", nil, nil, nil)
			require.ErrorIs(t, err, context.Canceled)
			assert.Zero(t, requests.Load())
		}},
		{"HTTP2 fallback retains cancellation", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			protocols := make(chan int, 2)
			canceled := make(chan struct{}, 1)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				protocols <- r.ProtoMajor
				assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
				assert.Equal(t, "tenant", r.URL.Query().Get("tenantId"))
				if r.ProtoMajor == 2 {
					w.WriteHeader(http.StatusOK)
					assert.NoError(t, http.NewResponseController(w).Flush())
					panic(http.ErrAbortHandler)
				}
				cancel()
				select {
				case <-r.Context().Done():
					canceled <- struct{}{}
				case <-time.After(time.Second):
				}
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()
			client := NewClient(server.URL, "org", "token", nil, false)
			transport := server.Client().Transport.(*http.Transport).Clone()
			transport.Protocols = new(http.Protocols)
			transport.Protocols.SetHTTP1(true)
			transport.Protocols.SetHTTP2(true)
			client.HTTPClient.Transport = transport
			_, _, err := client.DoWithContext(ctx, http.MethodGet, "/v2/org/{org}/nico/vpc", nil, map[string]string{"tenantId": "tenant"}, nil)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, 2, <-protocols)
			require.Equal(t, 1, <-protocols)
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Error("fallback HTTP request did not receive cancellation")
			}
		}},
		{"cancellation during refresh prevents token publication and replay", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusUnauthorized) }))
			defer server.Close()
			client := NewClient(server.URL, "org", "original", nil, false)
			refreshes := 0
			client.TokenRefresh = func() (string, error) { refreshes++; cancel(); return "refreshed", nil }
			_, _, err := client.DoWithContext(ctx, http.MethodGet, "/v2/org/{org}/nico/vpc", nil, nil, nil)
			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, 1, refreshes)
			assert.EqualValues(t, 1, requests.Load())
			assert.Equal(t, "original", client.Token)
		}},
	} {
		t.Run(tc.name, tc.check)
	}
}
