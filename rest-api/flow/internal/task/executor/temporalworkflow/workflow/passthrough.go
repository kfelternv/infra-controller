// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package workflow

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/NVIDIA/infra-controller/rest-api/common/pkg/nicopassthrough"
	"github.com/NVIDIA/infra-controller/rest-api/flow/internal/task/executor/temporalworkflow/activity"
)

// init registers the NICo Core passthrough as an internal workflow (no
// TaskType): it is started directly by name from the REST API, not dispatched
// through the component-task Execute() path.
func init() {
	register(WorkflowDescriptor{
		WorkflowName: nicopassthrough.WorkflowName,
		WorkflowFunc: invokeCorePassthrough,
	})
}

// corePassthroughActivityOptions intentionally disables automatic retries: a
// passthrough request may be a non-idempotent mutation, so the activity runs
// exactly once and the caller decides whether to retry.
var corePassthroughActivityOptions = workflow.ActivityOptions{
	StartToCloseTimeout: 2 * time.Minute,
	RetryPolicy: &temporal.RetryPolicy{
		MaximumAttempts: 1,
	},
}

// invokeCorePassthrough is a thin workflow that runs the passthrough activity
// on the Flow worker (which holds the direct gRPC connection to NICo Core) and
// returns its result to the REST API.
func invokeCorePassthrough(
	ctx workflow.Context,
	req nicopassthrough.Request,
) (nicopassthrough.Response, error) {
	ctx = workflow.WithActivityOptions(ctx, corePassthroughActivityOptions)

	var resp nicopassthrough.Response
	err := workflow.ExecuteActivity(ctx, activity.NameInvokeCorePassthrough, req).Get(ctx, &resp)
	if err != nil {
		return nicopassthrough.Response{}, err
	}
	return resp, nil
}
