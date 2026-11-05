// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"testing"
	"time"

	"github.com/NVIDIA/infra-controller/rest-api/db/pkg/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDomainSQLDAO_CompleteRecovery(t *testing.T) {
	for _, tc := range []struct {
		name string
	}{
		{"live claim"},
		{"expired lease"},
		{"different token"},
		{"different Core ID"},
		{"changed lifecycle state"},
		{"deleted reservation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			session := testDomainInitDB(t)
			t.Cleanup(session.Close)
			testDomainSetupSchema(t, session)
			user := testDomainBuildUser(t, session, "completion-user")
			dao := NewDomainDAO(session)
			tenant, site, coreID := uuid.New(), uuid.New(), uuid.New()
			row, err := db.WithTxResult(ctx, session, func(tx *db.Tx) (*Domain, error) {
				row, fresh, err := dao.ReserveOwned(ctx, tx, DomainCreateInput{
					Hostname: "completion.example.com", Org: "completion-org", TenantID: &tenant, SiteID: &site,
					ControllerDomainID: &coreID, Status: DomainStatusPending, CreatedBy: user.ID,
				})
				require.True(t, fresh)
				return row, err
			})
			require.NoError(t, err)
			_, err = session.DB.ExecContext(ctx, `UPDATE domain SET recovery_next_at = current_timestamp - interval '1 second' WHERE id = ?`, row.ID)
			require.NoError(t, err)
			claims, err := dao.ClaimRecovery(ctx, 1, time.Minute)
			require.NoError(t, err)
			require.Len(t, claims, 1)
			require.NotNil(t, claims[0].RecoveryToken)
			token := *claims[0].RecoveryToken
			switch tc.name {
			case "expired lease":
				_, err = session.DB.ExecContext(ctx, `UPDATE domain SET recovery_lease_until = current_timestamp - interval '1 second' WHERE id = ?`, row.ID)
			case "different token":
				token = uuid.New()
			case "different Core ID":
				coreID = uuid.New()
			case "changed lifecycle state":
				_, err = session.DB.ExecContext(ctx, `UPDATE domain SET status = ? WHERE id = ?`, DomainStatusRejecting, row.ID)
			case "deleted reservation":
				err = dao.Delete(ctx, nil, row.ID)
			}
			require.NoError(t, err)
			before := &Domain{}
			require.NoError(t, session.DB.NewSelect().Model(before).Where("id = ?", row.ID).WhereAllWithDeleted().Scan(ctx))
			changed, err := dao.CompleteRecovery(ctx, row.ID, coreID, token, DomainStatusPending, DomainStatusReady, false)
			require.NoError(t, err)
			after := &Domain{}
			require.NoError(t, session.DB.NewSelect().Model(after).Where("id = ?", row.ID).WhereAllWithDeleted().Scan(ctx))
			if tc.name != "live claim" {
				require.False(t, changed, "an obsolete completion must not mutate the reservation")
				require.Equal(t, before, after)
				return
			}
			require.True(t, changed)
			require.Equal(t, DomainStatusReady, after.Status)
			require.Equal(t, before.Hostname, after.Hostname)
			require.Equal(t, before.TenantID, after.TenantID)
			require.Equal(t, before.SiteID, after.SiteID)
			require.Equal(t, before.ControllerDomainID, after.ControllerDomainID)
			require.Equal(t, before.RecoveryAttempts, after.RecoveryAttempts)
			require.Nil(t, after.RecoveryToken)
			require.Nil(t, after.RecoveryLeaseUntil)
			require.Nil(t, after.RecoveryNextAt)
			require.Nil(t, after.Deleted)
		})
	}
}
