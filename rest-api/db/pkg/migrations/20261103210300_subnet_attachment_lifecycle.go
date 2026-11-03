// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(subnetAttachmentUpMigration, func(_ context.Context, _ *bun.DB) error {
		// Keep unresolved intents available to the previous workflow process on rollback.
		return nil
	})
}

var subnetAttachmentColumns = []struct {
	name    string
	types   []string
	notNull bool
}{
	{name: "attach_intent_id", types: []string{"uuid"}},
	{name: "attach_source_vpc_id", types: []string{"uuid"}},
	{name: "attach_target_vpc_id", types: []string{"uuid"}},
	{name: "attach_source_controller_vpc_id", types: []string{"uuid"}},
	{name: "attach_target_controller_vpc_id", types: []string{"uuid"}},
	{name: "attach_segment_version", types: []string{"text", "character varying"}},
	{name: "attach_recovery_token", types: []string{"uuid"}},
	{name: "attach_lease_until", types: []string{"timestamp with time zone"}},
	{name: "attach_next_at", types: []string{"timestamp with time zone"}},
	{name: "attach_attempts", types: []string{"integer", "bigint"}, notNull: true},
}

func subnetAttachmentUpMigration(ctx context.Context, db *bun.DB) error {
	// Fresh REST bootstrap creates columns from the current Bun model. Upgrades
	// add them here; replay must preserve an already recorded attachment intent.
	return db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			ALTER TABLE subnet
			ADD COLUMN IF NOT EXISTS attach_intent_id uuid,
			ADD COLUMN IF NOT EXISTS attach_source_vpc_id uuid,
			ADD COLUMN IF NOT EXISTS attach_target_vpc_id uuid,
			ADD COLUMN IF NOT EXISTS attach_source_controller_vpc_id uuid,
			ADD COLUMN IF NOT EXISTS attach_target_controller_vpc_id uuid,
			ADD COLUMN IF NOT EXISTS attach_segment_version text,
			ADD COLUMN IF NOT EXISTS attach_recovery_token uuid,
			ADD COLUMN IF NOT EXISTS attach_lease_until timestamptz,
			ADD COLUMN IF NOT EXISTS attach_next_at timestamptz,
			ADD COLUMN IF NOT EXISTS attach_attempts integer NOT NULL DEFAULT 0,
			ALTER COLUMN attach_attempts SET DEFAULT 0
		`); err != nil {
			return err
		}
		// Recreate only this migration's index so a partial older attempt cannot
		// leave a same-named index with a different recovery predicate.
		if _, err := tx.ExecContext(ctx, `
			DROP INDEX IF EXISTS subnet_attach_recovery_due_idx
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			CREATE INDEX subnet_attach_recovery_due_idx ON subnet (attach_next_at, updated, id)
			WHERE deleted IS NULL AND attach_intent_id IS NOT NULL
		`); err != nil {
			return err
		}
		// IF NOT EXISTS handles bootstrap, but must not accept incompatible drift.
		for _, column := range subnetAttachmentColumns {
			var dataType, isNullable string
			err := tx.QueryRowContext(ctx, `
				SELECT data_type, is_nullable FROM information_schema.columns
				WHERE table_schema = current_schema() AND table_name = 'subnet' AND column_name = ?
			`, column.name).Scan(&dataType, &isNullable)
			if err != nil {
				return fmt.Errorf("inspect subnet.%s: %w", column.name, err)
			}
			if !slices.Contains(column.types, dataType) {
				return fmt.Errorf("incompatible existing column subnet.%s: type %s, expected one of %v", column.name, dataType, column.types)
			}
			if column.notNull && isNullable != "NO" {
				return fmt.Errorf("incompatible existing column subnet.%s: nullable, expected NOT NULL", column.name)
			}
		}
		return nil
	})
}
