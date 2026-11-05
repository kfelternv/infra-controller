// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"context"
	"testing"
	"time"

	cutil "github.com/NVIDIA/infra-controller/rest-api/common/pkg/util"
	"github.com/NVIDIA/infra-controller/rest-api/db/pkg/db"
	"github.com/NVIDIA/infra-controller/rest-api/db/pkg/util"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAttachmentDAOFixture(t *testing.T) (*db.Session, SubnetDAO, SubnetAttachIntent) {
	t.Helper()
	s := util.GetTestDBSession(t, true)
	t.Cleanup(s.Close)
	TestSetupSchema(t, s)
	user := TestBuildUser(t, s, uuid.NewString(), "attachment-org", []string{"FORGE_TENANT_ADMIN"})
	provider := TestBuildInfrastructureProvider(t, s, "provider", "attachment-org", user)
	tenant := TestBuildTenant(t, s, "tenant", "attachment-org", user)
	site := TestBuildSite(t, s, provider, "site", user)
	source := TestBuildVPC(t, s, "source", provider, tenant, site, cutil.GetPtr(VpcEthernetVirtualizer), cutil.GetPtr(uuid.New()), nil, VpcStatusReady, user, nil)
	target := TestBuildVPC(t, s, "target", provider, tenant, site, cutil.GetPtr(VpcEthernetVirtualizer), cutil.GetPtr(uuid.New()), nil, VpcStatusReady, user, nil)
	block := TestBuildIPBlock(t, s, "block", site, tenant, IPBlockRoutingTypeDatacenterOnly, "192.0.2.0", 24, IPBlockProtocolVersionV4)
	subnet := TestBuildSubnet(t, s, "subnet", tenant, source, cutil.GetPtr(uuid.New()), block, SubnetStatusReady, user)
	return s, NewSubnetDAO(s), SubnetAttachIntent{ID: uuid.New(), SubnetID: subnet.ID, TenantID: tenant.ID, SiteID: site.ID, ControllerSegmentID: *subnet.ControllerNetworkSegmentID, SourceVpcID: source.ID, TargetVpcID: target.ID, SourceControllerVpcID: *source.ControllerVpcID, TargetControllerVpcID: *target.ControllerVpcID, SegmentVersion: "V1"}
}

func reserveAttachmentForTest(t *testing.T, s *db.Session, dao SubnetDAO, intent SubnetAttachIntent) {
	t.Helper()
	ok, err := db.WithTxResult(context.Background(), s, func(tx *db.Tx) (bool, error) { return dao.ReserveAttachment(context.Background(), tx, intent) })
	require.NoError(t, err)
	require.True(t, ok)
}

func TestSubnetSQLDAO_ReserveAttachment(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*SubnetAttachIntent)
		status string
		want   bool
	}{
		{name: "records complete immutable operation", want: true},
		{name: "rejects a different tenant", change: func(i *SubnetAttachIntent) { i.TenantID = uuid.New() }},
		{name: "rejects stale source ownership", change: func(i *SubnetAttachIntent) { i.SourceVpcID = uuid.New() }},
		{name: "rejects a replaced controller segment", change: func(i *SubnetAttachIntent) { i.ControllerSegmentID = uuid.New() }},
		{name: "does not reserve a deleting Subnet", status: SubnetStatusDeleting},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, dao, original := newAttachmentDAOFixture(t)
			intent := original
			if tc.change != nil {
				tc.change(&intent)
			}
			if tc.status != "" {
				_, err := dao.Update(ctx, nil, SubnetUpdateInput{SubnetId: intent.SubnetID, Status: &tc.status})
				require.NoError(t, err)
			}
			ok, err := db.WithTxResult(ctx, s, func(tx *db.Tx) (bool, error) { return dao.ReserveAttachment(ctx, tx, intent) })
			require.NoError(t, err)
			require.Equal(t, tc.want, ok)
			stored, err := dao.GetByID(ctx, nil, original.SubnetID, nil)
			require.NoError(t, err)
			assert.Equal(t, original.SourceVpcID, stored.VpcID)
			if !tc.want {
				assert.Nil(t, stored.AttachIntentID)
				return
			}
			assert.Equal(t, &intent.ID, stored.AttachIntentID)
			assert.Equal(t, &intent.SourceVpcID, stored.AttachSourceVpcID)
			assert.Equal(t, &intent.TargetVpcID, stored.AttachTargetVpcID)
			assert.Equal(t, &intent.SourceControllerVpcID, stored.AttachSourceControllerVpcID)
			assert.Equal(t, &intent.TargetControllerVpcID, stored.AttachTargetControllerVpcID)
			assert.Equal(t, &intent.SegmentVersion, stored.AttachSegmentVersion)
			assert.Nil(t, stored.AttachRecoveryToken)
			require.NotNil(t, stored.AttachNextAt)
			assert.True(t, stored.AttachNextAt.After(time.Now()))
			competitor := intent
			competitor.ID = uuid.New()
			ok, err = db.WithTxResult(ctx, s, func(tx *db.Tx) (bool, error) { return dao.ReserveAttachment(ctx, tx, competitor) })
			require.NoError(t, err)
			assert.False(t, ok)
			stored, err = dao.GetByID(ctx, nil, intent.SubnetID, nil)
			require.NoError(t, err)
			assert.Equal(t, &intent.ID, stored.AttachIntentID)
		})
	}
}

func TestSubnetSQLDAO_CompleteAttachment(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		change                 func(*SubnetAttachIntent)
		claimed, expired, want bool
	}{
		{name: "handler completes an unclaimed intent", want: true},
		{name: "different intent cannot complete", change: func(i *SubnetAttachIntent) { i.ID = uuid.New() }},
		{name: "different tenant cannot complete", change: func(i *SubnetAttachIntent) { i.TenantID = uuid.New() }},
		{name: "changed source cannot complete", change: func(i *SubnetAttachIntent) { i.SourceVpcID = uuid.New() }},
		{name: "handler cannot supersede a recovery claim", claimed: true},
		{name: "worker completes with its current claim", claimed: true, want: true, change: func(*SubnetAttachIntent) {}},
		{name: "expired claim cannot complete", claimed: true, expired: true, change: func(*SubnetAttachIntent) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, dao, original := newAttachmentDAOFixture(t)
			reserveAttachmentForTest(t, s, dao, original)
			intent := original
			if tc.claimed {
				_, err := s.DB.ExecContext(ctx, "UPDATE subnet SET attach_next_at = current_timestamp WHERE id = ?", intent.SubnetID)
				require.NoError(t, err)
				rows, err := dao.ClaimAttachmentRecovery(ctx, 1, time.Minute)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				if tc.change != nil {
					intent.RecoveryToken = rows[0].AttachRecoveryToken
				}
				if tc.expired {
					_, err = s.DB.ExecContext(ctx, "UPDATE subnet SET attach_lease_until = current_timestamp - interval '1 second' WHERE id = ?", intent.SubnetID)
					require.NoError(t, err)
				}
			}
			if tc.change != nil {
				tc.change(&intent)
			}
			ok, err := db.WithTxResult(ctx, s, func(tx *db.Tx) (bool, error) { return dao.CompleteAttachment(ctx, tx, intent) })
			require.NoError(t, err)
			require.Equal(t, tc.want, ok)
			stored, err := dao.GetByID(ctx, nil, original.SubnetID, nil)
			require.NoError(t, err)
			if tc.want {
				assert.Equal(t, original.TargetVpcID, stored.VpcID)
				assert.Nil(t, stored.AttachIntentID)
				assert.Nil(t, stored.AttachSourceVpcID)
				assert.Nil(t, stored.AttachTargetVpcID)
				assert.Nil(t, stored.AttachSourceControllerVpcID)
				assert.Nil(t, stored.AttachTargetControllerVpcID)
				assert.Nil(t, stored.AttachSegmentVersion)
				assert.Nil(t, stored.AttachRecoveryToken)
				assert.Nil(t, stored.AttachLeaseUntil)
				assert.Nil(t, stored.AttachNextAt)
			} else {
				assert.Equal(t, original.SourceVpcID, stored.VpcID)
				assert.Equal(t, &original.ID, stored.AttachIntentID)
			}
		})
	}
}

func TestSubnetSQLDAO_ClaimAttachmentRecovery(t *testing.T) {
	ctx := context.Background()
	s, dao, intent := newAttachmentDAOFixture(t)
	reserveAttachmentForTest(t, s, dao, intent)
	rows, err := dao.ClaimAttachmentRecovery(ctx, 1, time.Minute)
	require.NoError(t, err)
	require.Empty(t, rows, "new handler reservation is not due")
	_, err = s.DB.ExecContext(ctx, "UPDATE subnet SET attach_next_at = current_timestamp WHERE id = ?", intent.SubnetID)
	require.NoError(t, err)
	tx, err := db.BeginTx(ctx, s, nil)
	require.NoError(t, err)
	defer tx.Rollback() // Release the test-owned lock even if an assertion fails.
	var locked uuid.UUID
	err = db.GetIDB(tx, s).NewRaw("SELECT id FROM subnet WHERE id = ? FOR UPDATE", intent.SubnetID).Scan(ctx, &locked)
	require.NoError(t, err)
	claimCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	rows, err = dao.ClaimAttachmentRecovery(claimCtx, 1, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, rows, "another replica owns the row lock")
	require.NoError(t, tx.Commit())
	rows, err = dao.ClaimAttachmentRecovery(ctx, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	first := rows[0]
	require.NotNil(t, first.AttachRecoveryToken)
	assert.Equal(t, 1, first.AttachAttempts)
	rows, err = dao.ClaimAttachmentRecovery(ctx, 1, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, rows, "a live lease must exclude another replica")
	_, err = s.DB.ExecContext(ctx, "UPDATE subnet SET attach_lease_until = current_timestamp - interval '1 second' WHERE id = ?", intent.SubnetID)
	require.NoError(t, err)
	rows, err = dao.ClaimAttachmentRecovery(ctx, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	second := rows[0]
	require.NotNil(t, second.AttachRecoveryToken)
	assert.NotEqual(t, *first.AttachRecoveryToken, *second.AttachRecoveryToken)
	assert.Equal(t, 2, second.AttachAttempts)
	stale := intent
	stale.RecoveryToken = first.AttachRecoveryToken
	for _, operation := range []struct {
		name string
		run  func() (bool, error)
	}{
		{"complete", func() (bool, error) {
			return db.WithTxResult(ctx, s, func(tx *db.Tx) (bool, error) { return dao.CompleteAttachment(ctx, tx, stale) })
		}},
		{"cancel", func() (bool, error) {
			return db.WithTxResult(ctx, s, func(tx *db.Tx) (bool, error) { return dao.CancelAttachment(ctx, tx, stale) })
		}},
		{"defer", func() (bool, error) {
			return dao.DeferAttachmentRecovery(ctx, intent.SubnetID, intent.ID, *first.AttachRecoveryToken, time.Minute)
		}},
	} {
		t.Run("replaced claim cannot "+operation.name, func(t *testing.T) { changed, err := operation.run(); require.NoError(t, err); assert.False(t, changed) })
	}
	changed, err := dao.DeferAttachmentRecovery(ctx, intent.SubnetID, intent.ID, *second.AttachRecoveryToken, time.Minute)
	require.NoError(t, err)
	require.True(t, changed)
	stored, err := dao.GetByID(ctx, nil, intent.SubnetID, nil)
	require.NoError(t, err)
	assert.Equal(t, &intent.ID, stored.AttachIntentID)
	assert.Nil(t, stored.AttachRecoveryToken)
	assert.Nil(t, stored.AttachLeaseUntil)
	require.NotNil(t, stored.AttachNextAt)
	assert.True(t, stored.AttachNextAt.After(time.Now()))
}

func TestSubnetSQLDAO_UpdateVpcFromInventory(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		pending, stale, wrongSite bool
		want                      bool
	}{
		{name: "uncontended observation updates VPC and MTU", want: true},
		{name: "pending tenant intent takes precedence", pending: true},
		{name: "newer REST VPC takes precedence", stale: true},
		{name: "another Site cannot apply the observation", wrongSite: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, dao, intent := newAttachmentDAOFixture(t)
			if tc.pending {
				reserveAttachmentForTest(t, s, dao, intent)
			}
			expected, siteID := intent.SourceVpcID, intent.SiteID
			if tc.stale {
				expected = uuid.New()
			}
			if tc.wrongSite {
				siteID = uuid.New()
			}
			before, err := dao.GetByID(ctx, nil, intent.SubnetID, nil)
			require.NoError(t, err)
			changed, err := dao.UpdateVpcFromInventory(ctx, intent.SubnetID, siteID, expected, intent.TargetVpcID, nil, cutil.GetPtr(9000), true)
			require.NoError(t, err)
			assert.Equal(t, tc.want, changed)
			after, err := dao.GetByID(ctx, nil, intent.SubnetID, nil)
			require.NoError(t, err)
			if tc.want {
				assert.Equal(t, intent.TargetVpcID, after.VpcID)
				assert.Equal(t, cutil.GetPtr(9000), after.MTU)
			} else {
				assert.Equal(t, before, after, "rejected observation must not alter any stored field")
			}
		})
	}
}
