// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"encoding/json"

	"github.com/NVIDIA/infra-controller/rest-api/common/pkg/nicopassthrough"
)

// APINICoCorePassthroughRequest is the body for an admin NICo Core gRPC
// passthrough invocation.
type APINICoCorePassthroughRequest struct {
	// Method is the NICo Core gRPC method to invoke, either bare
	// ("FindMachineIds") or fully qualified ("/forge.Forge/FindMachineIds").
	Method string `json:"method"`

	// Request is the protojson-encoded request message for Method. Omit or send
	// {} for methods with an empty request.
	Request json.RawMessage `json:"request,omitempty"`

	// AllowMutation must be true to invoke a method classified as a
	// write/destructive operation. Read methods ignore this field.
	AllowMutation bool `json:"allowMutation,omitempty"`
}

// APINICoCorePassthroughResponse is the result of a passthrough invocation.
type APINICoCorePassthroughResponse struct {
	// Method is the bare method that was invoked.
	Method string `json:"method"`

	// Response is the protojson-encoded response message from NICo Core.
	Response json.RawMessage `json:"response,omitempty"`
}

// APINICoCoreMethodsResponse is the catalog of invocable NICo Core methods.
type APINICoCoreMethodsResponse struct {
	// Service is the fully qualified gRPC service name.
	Service string `json:"service"`

	// Methods is the list of invocable unary methods with their request and
	// response types and read/mutation classification.
	Methods []nicopassthrough.MethodInfo `json:"methods"`
}
