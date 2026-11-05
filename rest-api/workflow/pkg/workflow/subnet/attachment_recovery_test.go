// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package subnet

import (
	"context"
	"errors"
	"testing"
	"time"

	subnetActivity "github.com/NVIDIA/infra-controller/rest-api/workflow/pkg/activity/subnet"
	"github.com/NVIDIA/infra-controller/rest-api/workflow/pkg/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/testsuite"
)

func TestReconcileSubnetAttachmentIntents(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "dispatches the bounded recovery activity"},
		{name: "propagates failure without an immediate retry", err: errors.New("claim database unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			var manager subnetActivity.ManageSubnet
			env.RegisterActivity(manager.ReconcileAttachmentIntents)
			env.OnActivity(manager.ReconcileAttachmentIntents, mock.Anything).Return(func(ctx context.Context) error {
				assert.Equal(t, 13*time.Minute, activity.GetInfo(ctx).StartToCloseTimeout)
				return tc.err
			}).Once()
			env.ExecuteWorkflow(ReconcileSubnetAttachmentIntents)
			require.True(t, env.IsWorkflowCompleted())
			if tc.err == nil {
				require.NoError(t, env.GetWorkflowError())
			} else {
				require.ErrorContains(t, env.GetWorkflowError(), tc.err.Error())
			}
			env.AssertExpectations(t)
		})
	}
}

func TestExecuteReconcileSubnetAttachmentIntents(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "registers the cloud cron"},
		{name: "returns scheduling failure", err: errors.New("Temporal unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tcClient := &mocks.Client{}
			tcClient.On("ExecuteWorkflow", mock.Anything, mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
				return options.ID == "subnet-attachment-intent-recovery" && options.TaskQueue == queue.CloudTaskQueue && options.CronSchedule == "@every 2m"
			}), mock.Anything).Return(&mocks.WorkflowRun{}, tc.err).Once()
			require.ErrorIs(t, ExecuteReconcileSubnetAttachmentIntents(context.Background(), tcClient), tc.err)
			tcClient.AssertExpectations(t)
		})
	}
}
