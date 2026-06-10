// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package activity

import (
	"context"
	"errors"
	"fmt"

	"github.com/NVIDIA/infra-controller/rest-api/common/pkg/nicopassthrough"
)

// NameInvokeCorePassthrough is the Temporal activity name for the NICo Core
// gRPC passthrough.
const NameInvokeCorePassthrough = "InvokeNICoCorePassthrough"

// CoreInvoker invokes NICo Core (forge.Forge) gRPC methods by name and lists
// the available method catalog. It is satisfied by nicoapi.Passthrough.
type CoreInvoker interface {
	Invoke(ctx context.Context, method string, reqJSON []byte) ([]byte, error)
	ListMethods() ([]nicopassthrough.MethodInfo, error)
}

// InvokeCorePassthrough runs a single passthrough request against NICo Core.
// When req.List is set it returns the method catalog; otherwise it transcodes
// the request and invokes the named method. Mutation gating is enforced here
// as defense in depth — the REST API gates the same request before dispatch.
func (a *Activities) InvokeCorePassthrough(
	ctx context.Context,
	req nicopassthrough.Request,
) (nicopassthrough.Response, error) {
	if a.coreInvoker == nil {
		return nicopassthrough.Response{}, errors.New(
			"NICo Core passthrough is not configured on this Flow worker",
		)
	}

	if req.List {
		methods, err := a.coreInvoker.ListMethods()
		if err != nil {
			return nicopassthrough.Response{}, fmt.Errorf("list NICo Core methods: %w", err)
		}
		return nicopassthrough.Response{Methods: methods}, nil
	}

	if req.Method == "" {
		return nicopassthrough.Response{}, errors.New("method is required")
	}

	if nicopassthrough.IsMutation(req.Method) && !req.AllowMutation {
		return nicopassthrough.Response{}, fmt.Errorf(
			"method %q is a mutation and requires allowMutation", nicopassthrough.MethodName(req.Method),
		)
	}

	respJSON, err := a.coreInvoker.Invoke(ctx, req.Method, req.RequestJSON)
	if err != nil {
		return nicopassthrough.Response{}, err
	}
	return nicopassthrough.Response{ResponseJSON: respJSON}, nil
}
