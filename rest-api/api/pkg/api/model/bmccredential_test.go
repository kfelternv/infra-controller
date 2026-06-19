// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"testing"

	"github.com/stretchr/testify/assert"

	cwssaws "github.com/NVIDIA/infra-controller/rest-api/workflow-schema/schema/site-agent/workflows/v1"
)

func bmcStrPtr(s string) *string { return &s }

func TestAPIBMCCredentialRequestValidate(t *testing.T) {
	mac := bmcStrPtr("aa:bb:cc:dd:ee:ff")
	cases := []struct {
		name    string
		req     APIBMCCredentialRequest
		wantErr bool
	}{
		{"site-wide-root ok", APIBMCCredentialRequest{Kind: BMCCredentialKindSiteWideRoot, Password: "pw"}, false},
		{"bmc-root ok", APIBMCCredentialRequest{Kind: BMCCredentialKindBMCRoot, Password: "pw", MacAddress: mac}, false},
		{"bmc-root missing mac", APIBMCCredentialRequest{Kind: BMCCredentialKindBMCRoot, Password: "pw"}, true},
		{"missing password", APIBMCCredentialRequest{Kind: BMCCredentialKindSiteWideRoot}, true},
		{"invalid kind", APIBMCCredentialRequest{Kind: "nope", Password: "pw"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAPIBMCCredentialRequestToProto(t *testing.T) {
	req := APIBMCCredentialRequest{
		Kind:       BMCCredentialKindBMCRoot,
		Password:   "pw",
		Username:   bmcStrPtr("root"),
		MacAddress: bmcStrPtr("aa:bb:cc:dd:ee:ff"),
	}
	p := req.ToProto()
	assert.Equal(t, cwssaws.CredentialType_RootBmcByMacAddress, p.GetCredentialType())
	assert.Equal(t, "pw", p.GetPassword())
	assert.Equal(t, "root", p.GetUsername())
	assert.Equal(t, "aa:bb:cc:dd:ee:ff", p.GetMacAddress())

	site := APIBMCCredentialRequest{Kind: BMCCredentialKindSiteWideRoot, Password: "pw"}
	assert.Equal(t, cwssaws.CredentialType_SiteWideBmcRoot, site.ToProto().GetCredentialType())
}
