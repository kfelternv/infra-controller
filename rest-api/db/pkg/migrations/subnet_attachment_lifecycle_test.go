// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package migrations

import (
	"context"
	"testing"
	"time"

	cutil "github.com/NVIDIA/infra-controller/rest-api/common/pkg/util"
	"github.com/NVIDIA/infra-controller/rest-api/db/pkg/db"
	"github.com/NVIDIA/infra-controller/rest-api/db/pkg/db/model"
	"github.com/NVIDIA/infra-controller/rest-api/db/pkg/util"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/migrate"
)

func newSubnetAttachmentMigrator(t *testing.T, ctx context.Context, session *db.Session) *migrate.Migrator {
	t.Helper()
	target := migrate.NewMigrations()
	for _, migration := range Migrations.Sorted() {
		if migration.Name == "20261103210300" {
			target.Add(migration)
		}
	}
	require.Len(t, target.Sorted(), 1)
	migrator := migrate.NewMigrator(session.DB, target,
		migrate.WithTableName("subnet_attachment_migrations_test"),
		migrate.WithLocksTableName("subnet_attachment_migration_locks_test"),
		migrate.WithMarkAppliedOnSuccess(true))
	require.NoError(t, migrator.Init(ctx))
	return migrator
}

func assertSubnetAttachmentSchema(t *testing.T, ctx context.Context, session *db.Session) {
	t.Helper()
	var count int
	err := session.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'subnet' AND column_name LIKE 'attach_%'`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 10, count)
	var counterDefault, nullable string
	err = session.DB.QueryRowContext(ctx, `SELECT column_default, is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'subnet' AND column_name = 'attach_attempts'`).Scan(&counterDefault, &nullable)
	require.NoError(t, err)
	assert.Equal(t, "0", counterDefault)
	assert.Equal(t, "NO", nullable)
	var indexDefinition string
	err = session.DB.QueryRowContext(ctx, `SELECT pg_get_indexdef('subnet_attach_recovery_due_idx'::regclass)`).Scan(&indexDefinition)
	require.NoError(t, err)
	assert.Contains(t, indexDefinition, "(attach_next_at, updated, id)")
	assert.Contains(t, indexDefinition, "(deleted IS NULL) AND (attach_intent_id IS NOT NULL)")
}

func newSubnetAttachmentMigrationFixture(t *testing.T, session *db.Session) *model.Subnet {
	t.Helper()
	user := model.TestBuildUser(t, session, uuid.NewString(), "attachment-migration", []string{"FORGE_TENANT_ADMIN"})
	provider := model.TestBuildInfrastructureProvider(t, session, "provider", "attachment-migration", user)
	tenant := model.TestBuildTenant(t, session, "tenant", "attachment-migration", user)
	site := model.TestBuildSite(t, session, provider, "site", user)
	vpc := model.TestBuildVPC(t, session, "source", provider, tenant, site, cutil.GetPtr(model.VpcEthernetVirtualizer), cutil.GetPtr(uuid.New()), nil, model.VpcStatusReady, user, nil)
	block := model.TestBuildIPBlock(t, session, "block", site, tenant, model.IPBlockRoutingTypeDatacenterOnly, "192.0.2.0", 24, model.IPBlockProtocolVersionV4)
	return model.TestBuildSubnet(t, session, "subnet", tenant, vpc, cutil.GetPtr(uuid.New()), block, model.SubnetStatusReady, user)
}

func TestSubnetAttachmentMigrationFreshInstall(t *testing.T) {
	ctx := context.Background()
	session := util.GetTestDBSession(t, true)
	defer session.Close()
	migrator := migrate.NewMigrator(session.DB, Migrations, migrate.WithMarkAppliedOnSuccess(true))
	require.NoError(t, migrator.Init(ctx))
	group, err := migrator.Migrate(ctx)
	require.NoError(t, err)
	require.False(t, group.IsZero())
	assertSubnetAttachmentSchema(t, ctx, session)
	group, err = migrator.Migrate(ctx)
	require.NoError(t, err)
	require.True(t, group.IsZero())
}

func TestSubnetAttachmentMigrationUpgradePreservesRows(t *testing.T) {
	ctx := context.Background()
	session := util.GetTestDBSession(t, true)
	defer session.Close()
	model.TestSetupSchema(t, session)
	subnet := newSubnetAttachmentMigrationFixture(t, session)
	_, err := session.DB.ExecContext(ctx, `ALTER TABLE subnet
		DROP COLUMN attach_intent_id, DROP COLUMN attach_source_vpc_id, DROP COLUMN attach_target_vpc_id,
		DROP COLUMN attach_source_controller_vpc_id, DROP COLUMN attach_target_controller_vpc_id,
		DROP COLUMN attach_segment_version, DROP COLUMN attach_recovery_token,
		DROP COLUMN attach_lease_until, DROP COLUMN attach_next_at, DROP COLUMN attach_attempts`)
	require.NoError(t, err)
	group, err := newSubnetAttachmentMigrator(t, ctx, session).Migrate(ctx)
	require.NoError(t, err)
	require.Len(t, group.Migrations, 1)
	assertSubnetAttachmentSchema(t, ctx, session)

	// Read through a fresh pool after the schema change, as restarted services do.
	migrated := util.GetTestDBSession(t, false)
	defer migrated.Close()
	stored, err := model.NewSubnetDAO(migrated).GetByID(ctx, nil, subnet.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, subnet.VpcID, stored.VpcID)
	assert.Equal(t, subnet.TenantID, stored.TenantID)
	assert.Equal(t, subnet.SiteID, stored.SiteID)
	assert.Equal(t, subnet.ControllerNetworkSegmentID, stored.ControllerNetworkSegmentID)
	assert.Zero(t, stored.AttachAttempts)
	assert.Nil(t, stored.AttachIntentID)

	// An older writer omits the new columns and receives usable defaults.
	oldWriter := *subnet
	oldWriter.ID = uuid.New()
	oldWriter.Name += "-old-writer"
	oldWriter.ControllerNetworkSegmentID = cutil.GetPtr(uuid.New())
	columns := make([]string, 0, len(subnetAttachmentColumns))
	for _, column := range subnetAttachmentColumns {
		columns = append(columns, column.name)
	}
	_, err = migrated.DB.NewInsert().Model(&oldWriter).ExcludeColumn(columns...).Exec(ctx)
	require.NoError(t, err)
	stored, err = model.NewSubnetDAO(migrated).GetByID(ctx, nil, oldWriter.ID, nil)
	require.NoError(t, err)
	assert.Zero(t, stored.AttachAttempts)
	assert.Nil(t, stored.AttachIntentID)
}

func TestSubnetAttachmentMigrationReplayPreservesIntent(t *testing.T) {
	ctx := context.Background()
	session := util.GetTestDBSession(t, true)
	defer session.Close()
	model.TestSetupSchema(t, session)
	subnet := newSubnetAttachmentMigrationFixture(t, session)
	lease := time.Now().UTC().Truncate(time.Microsecond).Add(time.Minute)
	_, err := session.DB.NewUpdate().Model(&model.Subnet{}).
		Set("attach_intent_id = ?", uuid.New()).Set("attach_source_vpc_id = ?", subnet.VpcID).
		Set("attach_target_vpc_id = ?", uuid.New()).Set("attach_source_controller_vpc_id = ?", uuid.New()).
		Set("attach_target_controller_vpc_id = ?", uuid.New()).Set("attach_segment_version = ?", "version-7").
		Set("attach_recovery_token = ?", uuid.New()).Set("attach_lease_until = ?", lease).
		Set("attach_next_at = ?", lease).Set("attach_attempts = 7").Where("id = ?", subnet.ID).Exec(ctx)
	require.NoError(t, err)
	before, err := model.NewSubnetDAO(session).GetByID(ctx, nil, subnet.ID, nil)
	require.NoError(t, err)
	_, err = session.DB.ExecContext(ctx, `CREATE INDEX subnet_attach_recovery_due_idx ON subnet (id)`)
	require.NoError(t, err)
	group, err := newSubnetAttachmentMigrator(t, ctx, session).Migrate(ctx)
	require.NoError(t, err)
	require.Len(t, group.Migrations, 1)
	require.NoError(t, subnetAttachmentUpMigration(ctx, session.DB))
	assertSubnetAttachmentSchema(t, ctx, session)
	after, err := model.NewSubnetDAO(session).GetByID(ctx, nil, subnet.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, before, after, "migration replay must not change the durable operation or its owner")
}

func TestSubnetAttachmentMigrationRejectsIncompatibleColumns(t *testing.T) {
	for _, tc := range []struct{ name, sql, message string }{
		{"wrong intent type", "ALTER TABLE subnet ALTER COLUMN attach_intent_id TYPE text USING attach_intent_id::text", "incompatible existing column subnet.attach_intent_id: type text"},
		{"nullable counter", "ALTER TABLE subnet ALTER COLUMN attach_attempts DROP NOT NULL", "incompatible existing column subnet.attach_attempts: nullable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			session := util.GetTestDBSession(t, true)
			defer session.Close()
			model.TestSetupSchema(t, session)
			_, err := session.DB.ExecContext(ctx, tc.sql)
			require.NoError(t, err)
			_, err = newSubnetAttachmentMigrator(t, ctx, session).Migrate(ctx)
			require.ErrorContains(t, err, tc.message)
			var indexes int
			err = session.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'subnet_attach_recovery_due_idx'`).Scan(&indexes)
			require.NoError(t, err)
			assert.Zero(t, indexes, "failed migration must roll back its index changes")
		})
	}
}
