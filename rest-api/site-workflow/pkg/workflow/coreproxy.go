// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package workflow

import (
	"time"

	"github.com/NVIDIA/infra-controller/rest-api/common/pkg/coreproxy"
	"github.com/NVIDIA/infra-controller/rest-api/site-workflow/pkg/activity"
	"github.com/rs/zerolog/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// InvokeCoreGRPC is the single generic workflow that proxies a curated REST
// request to a NICo Core (forge.Forge) gRPC method on the site. It replaces the
// per-resource workflow/activity pairs: the cloud handler validates and builds
// the typed request, and this workflow forwards it to the site activity that
// holds the Core connection.
//
// The function name must match coreproxy.WorkflowName.
func InvokeCoreGRPC(ctx workflow.Context, req coreproxy.Request) (coreproxy.Response, error) {
	logger := log.With().Str("Workflow", "InvokeCoreGRPC").Str("Method", req.FullMethod).Logger()
	logger.Info().Msg("Starting workflow")

	// No automatic retries: a proxied call may be a non-idempotent mutation, so
	// the activity runs exactly once and the caller decides whether to retry.
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var manager activity.ManageCoreProxy
	var resp coreproxy.Response
	err := workflow.ExecuteActivity(ctx, manager.InvokeCoreGRPCOnSite, req).Get(ctx, &resp)
	if err != nil {
		logger.Error().Err(err).Str("Activity", "InvokeCoreGRPCOnSite").Msg("Failed to execute activity from workflow")
		return coreproxy.Response{}, err
	}

	logger.Info().Msg("Completing workflow")
	return resp, nil
}
