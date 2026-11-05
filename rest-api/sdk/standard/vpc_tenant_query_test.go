// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package standard

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVpcAPIService_GetAllVPC(t *testing.T) {
	for _, tc := range []struct{ name, tenant string }{
		{name: "ordinary list omits the optional Tenant assertion"},
		{name: "Tenant assertion is encoded with existing selectors", tenant: "550e8400-e29b-41d4-a716-446655440000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &sdkContractTransport{status: http.StatusOK, responseBody: "[]"}
			client := newSDKContractClient(transport)
			request := client.VPCAPI.GetAllVpc(context.Background(), "tenant-org").SiteId("site-id").PageNumber(2).PageSize(25)
			expected := url.Values{"siteId": []string{"site-id"}, "pageNumber": []string{"2"}, "pageSize": []string{"25"}}
			if tc.tenant != "" {
				request = request.TenantId(tc.tenant)
				expected.Set("tenantId", tc.tenant)
			}
			rows, _, err := request.Execute()
			require.NoError(t, err)
			require.Empty(t, rows)
			require.NotNil(t, transport.request)
			require.Equal(t, http.MethodGet, transport.request.Method)
			require.Equal(t, "/v2/org/tenant-org/nico/vpc", transport.request.URL.Path)
			require.Equal(t, expected, transport.request.URL.Query())
			require.Empty(t, transport.body)
		})
	}
}
