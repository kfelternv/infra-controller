// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"fmt"

	cwssaws "github.com/NVIDIA/infra-controller/rest-api/workflow-schema/schema/site-agent/workflows/v1"
)

// BMC credential kinds exposed by the REST API. These map to the admin CLI
// `credential add-bmc --kind=...` surface.
const (
	// BMCCredentialKindSiteWideRoot stores the site-wide BMC root credential
	// (empty username). Maps to CredentialType_SiteWideBmcRoot.
	BMCCredentialKindSiteWideRoot = "site-wide-root"
	// BMCCredentialKindBMCRoot stores a per-BMC root credential keyed by MAC
	// address. Maps to CredentialType_RootBmcByMacAddress.
	BMCCredentialKindBMCRoot = "bmc-root"
)

// APIBMCCredentialRequest sets (creates or overwrites) a BMC credential.
type APIBMCCredentialRequest struct {
	// Kind selects which BMC credential to store: "site-wide-root" or "bmc-root".
	Kind string `json:"kind"`
	// Password is the credential password (required).
	Password string `json:"password"`
	// Username is optional; Core defaults to "root" when omitted for bmc-root.
	Username *string `json:"username,omitempty"`
	// MacAddress is required for kind "bmc-root" and ignored for "site-wide-root".
	MacAddress *string `json:"macAddress,omitempty"`
}

// Validate checks the request shape before it is converted to a proto.
func (r *APIBMCCredentialRequest) Validate() error {
	if err := validateBMCCredentialKind(r.Kind, r.MacAddress); err != nil {
		return err
	}
	if r.Password == "" {
		return fmt.Errorf("password is required")
	}
	return nil
}

// ToProto converts the validated request into the forge.Forge
// CredentialCreationRequest.
func (r *APIBMCCredentialRequest) ToProto() *cwssaws.CredentialCreationRequest {
	return &cwssaws.CredentialCreationRequest{
		CredentialType: bmcCredentialTypeForKind(r.Kind),
		Password:       r.Password,
		Username:       r.Username,
		MacAddress:     r.MacAddress,
	}
}

func validateBMCCredentialKind(kind string, macAddress *string) error {
	switch kind {
	case BMCCredentialKindSiteWideRoot:
		return nil
	case BMCCredentialKindBMCRoot:
		if macAddress == nil || *macAddress == "" {
			return fmt.Errorf("macAddress is required for kind %q", BMCCredentialKindBMCRoot)
		}
		return nil
	default:
		return fmt.Errorf("invalid kind %q (expected %q or %q)", kind, BMCCredentialKindSiteWideRoot, BMCCredentialKindBMCRoot)
	}
}

func bmcCredentialTypeForKind(kind string) cwssaws.CredentialType {
	switch kind {
	case BMCCredentialKindBMCRoot:
		return cwssaws.CredentialType_RootBmcByMacAddress
	default:
		return cwssaws.CredentialType_SiteWideBmcRoot
	}
}
