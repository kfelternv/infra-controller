// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package nicopassthrough holds the contract shared between the REST API and
// the on-site Flow worker for the NICo Core gRPC passthrough: the Temporal
// workflow name and task queue, the request/response envelope, the method
// catalog entry, and the read/mutation classifier.
//
// The passthrough lets a Provider Admin invoke any NICo Core (forge.Forge)
// gRPC method by name with a JSON-encoded request, without a hand-written REST
// endpoint per operation. The REST API gates the call and starts the Temporal
// workflow on the site; the Flow worker (which holds the direct mutual-TLS gRPC
// connection to Core) performs the JSON<->protobuf transcoding and the actual
// gRPC invocation.
package nicopassthrough

import (
	"encoding/json"
	"strings"
)

const (
	// ServiceName is the fully qualified gRPC service exposed by NICo Core.
	ServiceName = "forge.Forge"

	// WorkflowName is the Temporal workflow registered by the Flow worker and
	// started by the REST API to drive a single passthrough invocation.
	WorkflowName = "InvokeNICoCorePassthrough"

	// TaskQueue is the Temporal task queue the Flow worker polls. It must match
	// flow/internal/task/executor/temporalworkflow/manager.WorkflowQueue.
	TaskQueue = "flow-tasks"
)

// Request is the Temporal workflow input describing one passthrough call.
type Request struct {
	// Method is the Core gRPC method, either bare ("FindMachineIds") or fully
	// qualified ("/forge.Forge/FindMachineIds").
	Method string `json:"method,omitempty"`

	// RequestJSON is the protojson-encoded request message for Method. An empty
	// value is treated as the zero-valued request message.
	RequestJSON json.RawMessage `json:"requestJson,omitempty"`

	// AllowMutation must be set to true to run a method classified as a
	// mutation. Read methods ignore this field.
	AllowMutation bool `json:"allowMutation,omitempty"`

	// List, when true, returns the Core method catalog instead of invoking a
	// method. Method and RequestJSON are ignored.
	List bool `json:"list,omitempty"`
}

// Response is the Temporal workflow output for a passthrough call.
type Response struct {
	// ResponseJSON is the protojson-encoded response message, set for invoke
	// calls.
	ResponseJSON json.RawMessage `json:"responseJson,omitempty"`

	// Methods is the Core method catalog, set for List calls.
	Methods []MethodInfo `json:"methods,omitempty"`
}

// MethodInfo describes a single invocable Core gRPC method.
type MethodInfo struct {
	// Method is the bare method name (e.g. "FindMachineIds").
	Method string `json:"method"`
	// FullMethod is the fully qualified gRPC path (e.g. "/forge.Forge/FindMachineIds").
	FullMethod string `json:"fullMethod"`
	// InputType is the fully qualified protobuf message name of the request.
	InputType string `json:"inputType"`
	// OutputType is the fully qualified protobuf message name of the response.
	OutputType string `json:"outputType"`
	// Mutation reports whether this method is classified as a write/destructive
	// operation that requires AllowMutation.
	Mutation bool `json:"mutation"`
	// Deprecated reports whether the proto marks this method deprecated.
	Deprecated bool `json:"deprecated,omitempty"`
}

// readPrefixes are the method-name prefixes treated as read-only. Anything that
// does not match one of these is classified as a mutation, so newly added Core
// methods default to the safer (mutation, opt-in) side of the gate.
var readPrefixes = []string{
	"Find",
	"Get",
	"List",
	"Search",
	"Lookup",
	"Identify",
	"Determine",
	"Version",
	"Echo",
}

// MethodName returns the bare method name for a bare or fully qualified method.
func MethodName(method string) string {
	method = strings.TrimPrefix(method, "/")
	if i := strings.LastIndex(method, "/"); i >= 0 {
		return method[i+1:]
	}
	return method
}

// FullMethod returns the canonical "/forge.Forge/<Method>" gRPC path for a bare
// or already-qualified method name.
func FullMethod(method string) string {
	return "/" + ServiceName + "/" + MethodName(method)
}

// IsMutation reports whether method is classified as a write/destructive
// operation. The classifier is intentionally default-deny: only well-known
// read prefixes are treated as read-only; every other method (including ones
// not yet known to this code) is treated as a mutation.
func IsMutation(method string) bool {
	name := MethodName(method)
	for _, p := range readPrefixes {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	return true
}
