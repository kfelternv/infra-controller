// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package subnet

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	common "github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/handler/util/common"
	"github.com/NVIDIA/infra-controller/rest-api/common/pkg/grpcproxy"
	cutil "github.com/NVIDIA/infra-controller/rest-api/common/pkg/util"
	cdb "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db"
	cdbm "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db/model"
	corev1 "github.com/NVIDIA/infra-controller/rest-api/proto/core/gen/v1"
	swe "github.com/NVIDIA/infra-controller/rest-api/site-workflow/pkg/error"
	sc "github.com/NVIDIA/infra-controller/rest-api/workflow/pkg/client/site"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	temporalEnums "go.temporal.io/api/enums/v1"
	tmocks "go.temporal.io/sdk/mocks"
	tp "go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type attachmentRecoveryFixture struct {
	session             *cdb.Session
	subnet              *cdbm.Subnet
	intent              cdbm.SubnetAttachIntent
	claimed             cdbm.Subnet
	sourceControllerVpc uuid.UUID
	targetControllerVpc uuid.UUID
	siteClient          *tmocks.Client
	pool                *sc.ClientPool
}

func newAttachmentRecoveryFixture(t *testing.T) attachmentRecoveryFixture {
	t.Helper()
	ctx := context.Background()
	session := common.TestInitDB(t)
	t.Cleanup(session.Close)
	common.TestSetupSchema(t, session)
	user := common.TestBuildUser(t, session, uuid.NewString(), "recovery-org", []string{"FORGE_TENANT_ADMIN"})
	providerUser := common.TestBuildUser(t, session, uuid.NewString(), "provider-org", []string{"FORGE_PROVIDER_ADMIN"})
	provider := common.TestBuildInfrastructureProvider(t, session, "provider", "provider-org", providerUser)
	site := common.TestBuildSite(t, session, provider, "site", providerUser)
	_, err := cdbm.NewSiteDAO(session).Update(ctx, nil, cdbm.SiteUpdateInput{
		SiteID: site.ID, Status: cutil.GetPtr(cdbm.SiteStatusRegistered),
	})
	require.NoError(t, err)
	tenant := common.TestBuildTenant(t, session, "tenant", "recovery-org", user)
	common.TestBuildTenantSite(t, session, tenant, site, user)
	sourceControllerVpc := uuid.New()
	targetControllerVpc := uuid.New()
	sourceVpc := common.TestBuildVPC(t, session, "source", provider, tenant, site, &sourceControllerVpc, cutil.GetPtr(cdbm.VpcEthernetVirtualizer), nil, cdbm.VpcStatusReady, user)
	targetVpc := common.TestBuildVPC(t, session, "target", provider, tenant, site, &targetControllerVpc, cutil.GetPtr(cdbm.VpcEthernetVirtualizer), nil, cdbm.VpcStatusReady, user)
	segmentID := uuid.New()
	subnet := common.TestBuildSubnet(t, session, "subnet", tenant, sourceVpc, &segmentID, cdbm.SubnetStatusReady, user)
	intent := cdbm.SubnetAttachIntent{
		ID: uuid.New(), SubnetID: subnet.ID, TenantID: tenant.ID, SiteID: site.ID,
		ControllerSegmentID: segmentID, SourceVpcID: sourceVpc.ID, TargetVpcID: targetVpc.ID,
		SourceControllerVpcID: sourceControllerVpc, TargetControllerVpcID: targetControllerVpc,
		SegmentVersion: "V1",
	}
	dao := cdbm.NewSubnetDAO(session)
	tx, err := cdb.BeginTx(ctx, session, &sql.TxOptions{})
	require.NoError(t, err)
	reserved, err := dao.ReserveAttachment(ctx, tx, intent)
	require.NoError(t, err)
	require.True(t, reserved)
	require.NoError(t, tx.Commit())
	_, err = session.DB.ExecContext(ctx, `UPDATE subnet SET attach_next_at = current_timestamp - interval '1 second' WHERE id = ?`, subnet.ID)
	require.NoError(t, err)
	claimed, err := dao.ClaimAttachmentRecovery(ctx, 1, 2*time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NotNil(t, claimed[0].AttachRecoveryToken)

	client := &tmocks.Client{}
	pool := sc.NewClientPool(nil)
	pool.IDClientMap[site.ID.String()] = client
	return attachmentRecoveryFixture{
		session: session, subnet: subnet, intent: intent, claimed: claimed[0],
		sourceControllerVpc: sourceControllerVpc, targetControllerVpc: targetControllerVpc,
		siteClient: client, pool: pool,
	}
}

func (f *attachmentRecoveryFixture) segment(vpcID uuid.UUID, version string) *corev1.NetworkSegment {
	return &corev1.NetworkSegment{
		Id:     &corev1.NetworkSegmentId{Value: f.intent.ControllerSegmentID.String()},
		Config: &corev1.NetworkSegmentConfig{SegmentType: corev1.NetworkSegmentType_TENANT, VpcId: &corev1.VpcId{Value: vpcID.String()}},
		Status: &corev1.NetworkSegmentStatus{Lifecycle: &corev1.LifecycleStatus{Version: version}},
	}
}

func (f *attachmentRecoveryFixture) expectSegmentRead(t *testing.T, vpcID uuid.UUID, version string) {
	t.Helper()
	f.expectProxyResult(t, corev1.Forge_FindNetworkSegmentsByIds_FullMethodName, &corev1.NetworkSegmentList{NetworkSegments: []*corev1.NetworkSegment{f.segment(vpcID, version)}})
}

func (f *attachmentRecoveryFixture) expectProxyResult(t *testing.T, method string, response proto.Message) {
	t.Helper()
	run := &tmocks.WorkflowRun{}
	run.On("Get", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		payload, err := protojson.Marshal(response)
		require.NoError(t, err)
		args.Get(1).(*grpcproxy.Response).ResponseJSON = payload
	}).Return(nil).Once()
	f.siteClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, grpcproxy.Core.WorkflowName,
		mock.MatchedBy(func(request grpcproxy.Request) bool { return request.FullMethod == method })).Run(func(args mock.Arguments) {
		request := args.Get(3).(grpcproxy.Request)
		if method == corev1.Forge_FindNetworkSegmentsByIds_FullMethodName {
			var read corev1.NetworkSegmentsByIdsRequest
			require.NoError(t, protojson.Unmarshal(request.RequestJSON, &read))
			require.Len(t, read.NetworkSegmentsIds, 1)
			assert.Equal(t, f.intent.ControllerSegmentID.String(), read.NetworkSegmentsIds[0].GetValue())
		} else {
			var attach corev1.AttachNetworkSegmentToVpcRequest
			require.NoError(t, protojson.Unmarshal(request.RequestJSON, &attach))
			assert.Equal(t, f.intent.ControllerSegmentID.String(), attach.GetNetworkSegmentId().GetValue())
			assert.Equal(t, f.intent.TargetControllerVpcID.String(), attach.GetVpcId().GetValue())
			assert.True(t, attach.GetAllowReplace())
			assert.Equal(t, f.intent.SourceControllerVpcID.String(), attach.GetExpectedSourceVpcId().GetValue())
			assert.Equal(t, f.intent.SegmentVersion, attach.GetExpectedSegmentVersion())
		}
	}).Return(run, nil).Once()
}

func (f *attachmentRecoveryFixture) assertPending(t *testing.T) {
	t.Helper()
	stored, err := cdbm.NewSubnetDAO(f.session).GetByID(context.Background(), nil, f.subnet.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, &f.intent.ID, stored.AttachIntentID)
	assert.Equal(t, f.intent.SourceVpcID, stored.VpcID)
	assert.Equal(t, &f.intent.SourceControllerVpcID, stored.AttachSourceControllerVpcID)
	assert.Equal(t, &f.intent.TargetControllerVpcID, stored.AttachTargetControllerVpcID)
	assert.Equal(t, &f.intent.SegmentVersion, stored.AttachSegmentVersion)
	assert.Nil(t, stored.AttachRecoveryToken)
	assert.Nil(t, stored.AttachLeaseUntil)
	require.NotNil(t, stored.AttachNextAt)
	assert.True(t, stored.AttachNextAt.After(time.Now()))
}

func (f *attachmentRecoveryFixture) expectAttachError(err error) {
	run := &tmocks.WorkflowRun{}
	run.On("Get", mock.Anything, mock.Anything).Return(err).Once()
	f.siteClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, grpcproxy.Core.WorkflowName,
		mock.MatchedBy(func(request grpcproxy.Request) bool {
			return request.FullMethod == corev1.Forge_AttachNetworkSegmentToVpc_FullMethodName
		})).Return(run, nil).Once()
}

func TestManageSubnet_ReconcileAttachment(t *testing.T) {
	cases := []struct {
		name  string
		check func(*testing.T)
	}{
		{name: "version mismatch clears fenced intent", check: func(t *testing.T) {
			fixture := newAttachmentRecoveryFixture(t)
			fixture.expectSegmentRead(t, fixture.sourceControllerVpc, "V2")

			err := (ManageSubnet{dbSession: fixture.session, siteClientPool: fixture.pool}).reconcileAttachment(
				context.Background(), cdbm.NewSubnetDAO(fixture.session), &fixture.claimed,
			)
			require.NoError(t, err)
			stored, err := cdbm.NewSubnetDAO(fixture.session).GetByID(context.Background(), nil, fixture.subnet.ID, nil)
			require.NoError(t, err)
			assert.Nil(t, stored.AttachIntentID)
			assert.Equal(t, fixture.intent.SourceVpcID, stored.VpcID)
			fixture.siteClient.AssertExpectations(t)
		}},

		{name: "same-version third VPC stays pending", check: func(t *testing.T) {
			fixture := newAttachmentRecoveryFixture(t)
			fixture.expectSegmentRead(t, uuid.New(), fixture.intent.SegmentVersion)

			err := (ManageSubnet{dbSession: fixture.session, siteClientPool: fixture.pool}).reconcileAttachment(
				context.Background(), cdbm.NewSubnetDAO(fixture.session), &fixture.claimed,
			)
			require.ErrorContains(t, err, "third VPC")
			stored, err := cdbm.NewSubnetDAO(fixture.session).GetByID(context.Background(), nil, fixture.subnet.ID, nil)
			require.NoError(t, err)
			require.NotNil(t, stored.AttachIntentID)
			assert.Equal(t, fixture.intent.ID, *stored.AttachIntentID)
			assert.Nil(t, stored.AttachRecoveryToken)
			fixture.siteClient.AssertExpectations(t)
		}},

		{name: "typed Core refusal clears worker intent", check: func(t *testing.T) {
			fixture := newAttachmentRecoveryFixture(t)
			fixture.expectSegmentRead(t, fixture.sourceControllerVpc, fixture.intent.SegmentVersion)
			fixture.expectAttachError(tp.NewNonRetryableApplicationError(
				"overlapping prefix", swe.ErrTypeNICoFailedPrecondition, errors.New("overlapping prefix"),
			))

			err := (ManageSubnet{dbSession: fixture.session, siteClientPool: fixture.pool}).reconcileAttachment(
				context.Background(), cdbm.NewSubnetDAO(fixture.session), &fixture.claimed,
			)
			require.NoError(t, err)
			stored, err := cdbm.NewSubnetDAO(fixture.session).GetByID(context.Background(), nil, fixture.subnet.ID, nil)
			require.NoError(t, err)
			assert.Nil(t, stored.AttachIntentID)
			fixture.siteClient.AssertExpectations(t)
		}},

		{name: "ambiguous Core timeout retains worker intent", check: func(t *testing.T) {
			fixture := newAttachmentRecoveryFixture(t)
			fixture.expectSegmentRead(t, fixture.sourceControllerVpc, fixture.intent.SegmentVersion)
			fixture.expectAttachError(tp.NewTimeoutError(temporalEnums.TIMEOUT_TYPE_START_TO_CLOSE, nil, nil))

			err := (ManageSubnet{dbSession: fixture.session, siteClientPool: fixture.pool}).reconcileAttachment(
				context.Background(), cdbm.NewSubnetDAO(fixture.session), &fixture.claimed,
			)
			require.ErrorContains(t, err, "unconfirmed")
			stored, err := cdbm.NewSubnetDAO(fixture.session).GetByID(context.Background(), nil, fixture.subnet.ID, nil)
			require.NoError(t, err)
			require.NotNil(t, stored.AttachIntentID)
			assert.Equal(t, fixture.intent.ID, *stored.AttachIntentID)
			assert.Nil(t, stored.AttachRecoveryToken)
			fixture.siteClient.AssertExpectations(t)
		}},

		{name: "already applied target completes without another mutation", check: func(t *testing.T) {
			f := newAttachmentRecoveryFixture(t)
			f.expectSegmentRead(t, f.targetControllerVpc, "V2")
			err := (ManageSubnet{dbSession: f.session, siteClientPool: f.pool}).reconcileAttachment(context.Background(), cdbm.NewSubnetDAO(f.session), &f.claimed)
			require.NoError(t, err)
			stored, err := cdbm.NewSubnetDAO(f.session).GetByID(context.Background(), nil, f.subnet.ID, nil)
			require.NoError(t, err)
			assert.Equal(t, f.intent.TargetVpcID, stored.VpcID)
			assert.Nil(t, stored.AttachIntentID)
			assert.Nil(t, stored.AttachRecoveryToken)
			assert.Nil(t, stored.AttachLeaseUntil)
			assert.Nil(t, stored.AttachNextAt)
			f.siteClient.AssertExpectations(t)
			f.siteClient.AssertNumberOfCalls(t, "ExecuteWorkflow", 1)
		}},
		{name: "successful retry is fenced and independently read back", check: func(t *testing.T) {
			f := newAttachmentRecoveryFixture(t)
			f.expectSegmentRead(t, f.sourceControllerVpc, "V1")
			f.expectProxyResult(t, corev1.Forge_AttachNetworkSegmentToVpc_FullMethodName, f.segment(f.targetControllerVpc, "V2"))
			f.expectSegmentRead(t, f.targetControllerVpc, "V2")
			err := (ManageSubnet{dbSession: f.session, siteClientPool: f.pool}).reconcileAttachment(context.Background(), cdbm.NewSubnetDAO(f.session), &f.claimed)
			require.NoError(t, err)
			stored, err := cdbm.NewSubnetDAO(f.session).GetByID(context.Background(), nil, f.subnet.ID, nil)
			require.NoError(t, err)
			assert.Equal(t, f.intent.TargetVpcID, stored.VpcID)
			assert.Nil(t, stored.AttachIntentID)
			f.siteClient.AssertExpectations(t)
			f.siteClient.AssertNumberOfCalls(t, "ExecuteWorkflow", 3)
		}},
		{name: "successful response alone cannot override contrary readback", check: func(t *testing.T) {
			f := newAttachmentRecoveryFixture(t)
			f.expectSegmentRead(t, f.sourceControllerVpc, "V1")
			f.expectProxyResult(t, corev1.Forge_AttachNetworkSegmentToVpc_FullMethodName, f.segment(f.targetControllerVpc, "V2"))
			f.expectSegmentRead(t, f.sourceControllerVpc, "V1")
			err := (ManageSubnet{dbSession: f.session, siteClientPool: f.pool}).reconcileAttachment(context.Background(), cdbm.NewSubnetDAO(f.session), &f.claimed)
			require.ErrorContains(t, err, "target not confirmed")
			f.assertPending(t)
			f.siteClient.AssertExpectations(t)
		}},
		{name: "revoked tenant association prevents all Site calls", check: func(t *testing.T) {
			f := newAttachmentRecoveryFixture(t)
			_, err := f.session.DB.ExecContext(context.Background(), "DELETE FROM tenant_site WHERE tenant_id = ? AND site_id = ?", f.intent.TenantID, f.intent.SiteID)
			require.NoError(t, err)
			err = (ManageSubnet{dbSession: f.session, siteClientPool: f.pool}).reconcileAttachment(context.Background(), cdbm.NewSubnetDAO(f.session), &f.claimed)
			require.ErrorContains(t, err, "association not current")
			f.assertPending(t)
			f.siteClient.AssertNumberOfCalls(t, "ExecuteWorkflow", 0)
		}},
		{name: "unavailable Site prevents all Site calls", check: func(t *testing.T) {
			f := newAttachmentRecoveryFixture(t)
			_, err := cdbm.NewSiteDAO(f.session).Update(context.Background(), nil, cdbm.SiteUpdateInput{SiteID: f.intent.SiteID, Status: cutil.GetPtr(cdbm.SiteStatusError)})
			require.NoError(t, err)
			err = (ManageSubnet{dbSession: f.session, siteClientPool: f.pool}).reconcileAttachment(context.Background(), cdbm.NewSubnetDAO(f.session), &f.claimed)
			require.ErrorContains(t, err, "site is unavailable")
			f.assertPending(t)
			f.siteClient.AssertNumberOfCalls(t, "ExecuteWorkflow", 0)
		}},
		{name: "timeout is deferred then a later activity really retries", check: func(t *testing.T) {
			f := newAttachmentRecoveryFixture(t)
			f.expectSegmentRead(t, f.sourceControllerVpc, "V1")
			f.expectAttachError(tp.NewTimeoutError(temporalEnums.TIMEOUT_TYPE_START_TO_CLOSE, nil, nil))
			manager := ManageSubnet{dbSession: f.session, siteClientPool: f.pool}
			err := manager.reconcileAttachment(context.Background(), cdbm.NewSubnetDAO(f.session), &f.claimed)
			require.ErrorContains(t, err, "unconfirmed")
			f.assertPending(t)
			_, err = f.session.DB.ExecContext(context.Background(), "UPDATE subnet SET attach_next_at = current_timestamp WHERE id = ?", f.subnet.ID)
			require.NoError(t, err)
			f.expectSegmentRead(t, f.sourceControllerVpc, "V1")
			f.expectProxyResult(t, corev1.Forge_AttachNetworkSegmentToVpc_FullMethodName, f.segment(f.targetControllerVpc, "V2"))
			f.expectSegmentRead(t, f.targetControllerVpc, "V2")
			require.NoError(t, manager.ReconcileAttachmentIntents(context.Background()))
			stored, err := cdbm.NewSubnetDAO(f.session).GetByID(context.Background(), nil, f.subnet.ID, nil)
			require.NoError(t, err)
			assert.Equal(t, f.intent.TargetVpcID, stored.VpcID)
			assert.Nil(t, stored.AttachIntentID)
			assert.Equal(t, 2, stored.AttachAttempts)
			f.siteClient.AssertExpectations(t)
			f.siteClient.AssertNumberOfCalls(t, "ExecuteWorkflow", 5)
		}},
	}
	for _, tc := range []struct {
		name, message string
		change        func(*corev1.NetworkSegment)
	}{
		{"wrong Core segment", "identity not confirmed", func(s *corev1.NetworkSegment) { s.Id.Value = uuid.NewString() }},
		{"non tenant Core segment", "tenant segment version not confirmed", func(s *corev1.NetworkSegment) { s.Config.SegmentType = corev1.NetworkSegmentType_HOST_INBAND }},
		{"missing Core version", "tenant segment version not confirmed", func(s *corev1.NetworkSegment) { s.Status.Lifecycle.Version = "" }},
	} {
		cases = append(cases, struct {
			name  string
			check func(*testing.T)
		}{tc.name, func(t *testing.T) {
			f := newAttachmentRecoveryFixture(t)
			segment := f.segment(f.sourceControllerVpc, "V1")
			tc.change(segment)
			f.expectProxyResult(t, corev1.Forge_FindNetworkSegmentsByIds_FullMethodName, &corev1.NetworkSegmentList{NetworkSegments: []*corev1.NetworkSegment{segment}})
			err := (ManageSubnet{dbSession: f.session, siteClientPool: f.pool}).reconcileAttachment(context.Background(), cdbm.NewSubnetDAO(f.session), &f.claimed)
			require.ErrorContains(t, err, tc.message)
			f.assertPending(t)
			f.siteClient.AssertExpectations(t)
			f.siteClient.AssertNumberOfCalls(t, "ExecuteWorkflow", 1)
		}})
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.check)
	}
}

func TestCancelAttachmentRejectsStaleWorkerToken(t *testing.T) {
	fixture := newAttachmentRecoveryFixture(t)
	stale := fixture.intent
	stale.RecoveryToken = cutil.GetPtr(uuid.New())
	changed, err := cdb.WithTxResult(context.Background(), fixture.session, func(tx *cdb.Tx) (bool, error) {
		return cdbm.NewSubnetDAO(fixture.session).CancelAttachment(context.Background(), tx, stale)
	})
	require.NoError(t, err)
	assert.False(t, changed)
	stored, err := cdbm.NewSubnetDAO(fixture.session).GetByID(context.Background(), nil, fixture.subnet.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, stored.AttachIntentID)
	assert.Equal(t, fixture.intent.ID, *stored.AttachIntentID)
}
