// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	common "github.com/NVIDIA/infra-controller/rest-api/api/pkg/api/handler/util/common"
	"github.com/NVIDIA/infra-controller/rest-api/common/pkg/grpcproxy"
	"github.com/NVIDIA/infra-controller/rest-api/common/pkg/util"
	cdb "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db"
	cdbm "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db/model"
	corev1 "github.com/NVIDIA/infra-controller/rest-api/proto/core/gen/v1"
	swe "github.com/NVIDIA/infra-controller/rest-api/site-workflow/pkg/error"
	sc "github.com/NVIDIA/infra-controller/rest-api/workflow/pkg/client/site"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	tmocks "go.temporal.io/sdk/mocks"
	tp "go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestManageDomain_reconcileOne(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     string
		failure    string
		wantError  string
		wantStatus string
	}{
		{"revoked Site access resumes after reauthorization", cdbm.DomainStatusPending, "access", "owner lost Site access", cdbm.DomainStatusReady},
		{"unregistered Site resumes after registration", cdbm.DomainStatusPending, "registration", "Site is not registered", cdbm.DomainStatusReady},
		{"different Core ID cannot complete the reservation", cdbm.DomainStatusPending, "id", "different Domain", cdbm.DomainStatusReady},
		{"different Core name cannot complete the reservation", cdbm.DomainStatusPending, "name", "different Domain", cdbm.DomainStatusReady},
		{"unavailable create retries the same reserved ID", cdbm.DomainStatusPending, "transport", "create unconfirmed", cdbm.DomainStatusReady},
		{"lost rejection cancellation reply remains retriable", cdbm.DomainStatusRejecting, "transport", "rejection cancellation unconfirmed", cdbm.DomainStatusError},
		{"lost deletion reply remains retriable", cdbm.DomainStatusDeleting, "transport", "deletion/cancellation unconfirmed", cdbm.DomainStatusDeleting},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			session := common.TestInitDB(t)
			t.Cleanup(session.Close)
			common.TestSetupSchema(t, session)
			user := common.TestBuildUser(t, session, uuid.NewString(), "recovery-org", []string{"FORGE_TENANT_ADMIN"})
			providerUser := common.TestBuildUser(t, session, uuid.NewString(), "provider-org", []string{"FORGE_PROVIDER_ADMIN"})
			provider := common.TestBuildInfrastructureProvider(t, session, "provider", "provider-org", providerUser)
			site := common.TestBuildSite(t, session, provider, "site", providerUser)
			_, err := cdbm.NewSiteDAO(session).Update(ctx, nil, cdbm.SiteUpdateInput{SiteID: site.ID, Status: util.GetPtr(cdbm.SiteStatusRegistered)})
			require.NoError(t, err)
			tenant := common.TestBuildTenant(t, session, "tenant", "recovery-org", user)
			membership := common.TestBuildTenantSite(t, session, tenant, site, user)
			dao := cdbm.NewDomainDAO(session)
			coreID := uuid.New()
			row, err := cdb.WithTxResult(ctx, session, func(tx *cdb.Tx) (*cdbm.Domain, error) {
				row, fresh, err := dao.ReserveOwned(ctx, tx, cdbm.DomainCreateInput{
					Hostname: "recovery.example.com", Org: "recovery-org", TenantID: &tenant.ID, SiteID: &site.ID,
					ControllerDomainID: &coreID, Status: cdbm.DomainStatusPending, CreatedBy: user.ID,
				})
				require.True(t, fresh)
				return row, err
			})
			require.NoError(t, err)
			err = cdb.WithTx(ctx, session, func(tx *cdb.Tx) error {
				switch tc.status {
				case cdbm.DomainStatusRejecting:
					changed, err := dao.StageRejectedOwned(ctx, tx, row.ID, coreID, nil)
					require.True(t, changed)
					return err
				case cdbm.DomainStatusDeleting:
					reservedAt, err := dao.ReserveDeletionOwned(ctx, tx, row.ID, coreID, cdbm.DomainStatusPending, 90*time.Second)
					require.NotNil(t, reservedAt)
					return err
				default:
					return nil
				}
			})
			require.NoError(t, err)
			if tc.failure == "access" {
				require.NoError(t, cdbm.NewTenantSiteDAO(session).Delete(ctx, nil, membership.ID))
			}
			if tc.failure == "registration" {
				_, err = cdbm.NewSiteDAO(session).Update(ctx, nil, cdbm.SiteUpdateInput{SiteID: site.ID, Status: util.GetPtr(cdbm.SiteStatusPending)})
				require.NoError(t, err)
			}

			client := &tmocks.Client{}
			client.Test(t)
			pool := sc.NewClientPool(nil)
			pool.IDClientMap[site.ID.String()] = client
			manager := ManageDomain{DB: session, Sites: pool}
			var previousToken *uuid.UUID
			for attempt := range 2 {
				// Advance only this fixture's retry deadline; the actual DAO still
				// claims and fences each attempt with a new persisted lease/token.
				_, err = session.DB.ExecContext(ctx, `UPDATE domain SET recovery_next_at = current_timestamp - interval '1 second' WHERE id = ?`, row.ID)
				require.NoError(t, err)
				claims, err := dao.ClaimRecovery(ctx, 1, 120*time.Second)
				require.NoError(t, err)
				require.Len(t, claims, 1)
				claim := claims[0]
				require.NotNil(t, claim.RecoveryToken)
				require.NotEqual(t, previousToken, claim.RecoveryToken)
				previousToken = claim.RecoveryToken

				blocked := attempt == 0 && (tc.failure == "access" || tc.failure == "registration")
				if !blocked {
					run := &tmocks.WorkflowRun{}
					var resultErr error
					if attempt == 0 && tc.failure == "transport" {
						resultErr = tp.NewNonRetryableApplicationError("Site unavailable", swe.ErrTypeNICoUnavailable, errors.New("Site unavailable"))
					}
					client.On("ExecuteWorkflow", mock.Anything, mock.Anything, grpcproxy.Core.WorkflowName,
						mock.MatchedBy(func(request grpcproxy.Request) bool {
							if tc.status == cdbm.DomainStatusPending {
								var forwarded corev1.CreateDomainRequest
								return request.FullMethod == corev1.Forge_CreateDomain_FullMethodName &&
									protojson.Unmarshal(request.RequestJSON, &forwarded) == nil &&
									forwarded.GetReservedId().GetValue() == coreID.String() && forwarded.GetName() == row.Hostname
							}
							var forwarded corev1.DomainDeletionRequest
							return request.FullMethod == corev1.Forge_DeleteDomain_FullMethodName &&
								protojson.Unmarshal(request.RequestJSON, &forwarded) == nil &&
								forwarded.GetId().GetValue() == coreID.String() && forwarded.GetCancelReservedId()
						})).Return(run, nil).Once()
					run.On("Get", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
						if tc.status != cdbm.DomainStatusPending {
							return
						}
						response := &corev1.Domain{Id: &corev1.DomainId{Value: coreID.String()}, Name: strings.ToUpper(row.Hostname) + "."}
						if attempt == 0 && tc.failure == "id" {
							response.Id.Value = uuid.NewString()
						}
						if attempt == 0 && tc.failure == "name" {
							response.Name = "other.example.com"
						}
						payload, err := protojson.Marshal(response)
						require.NoError(t, err)
						args.Get(1).(*grpcproxy.Response).ResponseJSON = payload
					}).Return(resultErr).Once()
					t.Cleanup(func() { run.AssertExpectations(t) })
				}

				err = manager.reconcileOne(ctx, dao, &claim)
				stored := &cdbm.Domain{}
				require.NoError(t, session.DB.NewSelect().Model(stored).Where("id = ?", row.ID).WhereAllWithDeleted().Scan(ctx))
				require.Equal(t, row.Hostname, stored.Hostname)
				require.Equal(t, row.Org, stored.Org)
				require.Equal(t, row.TenantID, stored.TenantID)
				require.Equal(t, row.SiteID, stored.SiteID)
				require.Equal(t, row.ControllerDomainID, stored.ControllerDomainID)
				require.Equal(t, attempt+1, stored.RecoveryAttempts)
				require.Nil(t, stored.RecoveryToken)
				require.Nil(t, stored.RecoveryLeaseUntil)
				if attempt == 0 {
					require.ErrorContains(t, err, tc.wantError)
					require.Equal(t, tc.status, stored.Status)
					require.Nil(t, stored.Deleted)
					require.NotNil(t, stored.RecoveryNextAt)
					due, err := dao.ClaimRecovery(ctx, 1, time.Minute)
					require.NoError(t, err)
					require.Empty(t, due, "an uncertain operation must be deferred before the next retry")
					if blocked {
						client.AssertNumberOfCalls(t, "ExecuteWorkflow", 0)
					}
					if tc.failure == "access" {
						common.TestBuildTenantSite(t, session, tenant, site, user)
					}
					if tc.failure == "registration" {
						_, err = cdbm.NewSiteDAO(session).Update(ctx, nil, cdbm.SiteUpdateInput{SiteID: site.ID, Status: util.GetPtr(cdbm.SiteStatusRegistered)})
						require.NoError(t, err)
					}
				} else {
					require.NoError(t, err)
					require.Equal(t, tc.wantStatus, stored.Status)
					require.Nil(t, stored.RecoveryNextAt)
					if tc.status == cdbm.DomainStatusDeleting {
						require.NotNil(t, stored.Deleted)
						_, err = dao.GetByID(ctx, nil, row.ID, nil)
						require.ErrorIs(t, err, cdb.ErrDoesNotExist)
					} else {
						require.Nil(t, stored.Deleted)
					}
				}
			}
			client.AssertExpectations(t)
		})
	}
}

func TestManageDomain_ReconcileReservedDomains(t *testing.T) {
	for _, tc := range []struct {
		name        string
		firstFails  bool
		cancelFirst bool
	}{
		{name: "drains multiple due reservations"},
		{name: "a failed reservation does not delay other due work", firstFails: true},
		{name: "caller cancellation stops further claims", cancelFirst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session := common.TestInitDB(t)
			t.Cleanup(session.Close)
			common.TestSetupSchema(t, session)
			user := common.TestBuildUser(t, session, uuid.NewString(), "sweep-org", []string{"FORGE_TENANT_ADMIN"})
			providerUser := common.TestBuildUser(t, session, uuid.NewString(), "provider-org", []string{"FORGE_PROVIDER_ADMIN"})
			provider := common.TestBuildInfrastructureProvider(t, session, "provider", "provider-org", providerUser)
			site := common.TestBuildSite(t, session, provider, "site", providerUser)
			_, err := cdbm.NewSiteDAO(session).Update(ctx, nil, cdbm.SiteUpdateInput{
				SiteID: site.ID, Status: util.GetPtr(cdbm.SiteStatusRegistered),
			})
			require.NoError(t, err)
			tenant := common.TestBuildTenant(t, session, "tenant", "sweep-org", user)
			common.TestBuildTenantSite(t, session, tenant, site, user)
			dao := cdbm.NewDomainDAO(session)
			rows := make([]*cdbm.Domain, 0, 2)
			for _, name := range []string{"first.example.com", "second.example.com"} {
				coreID := uuid.New()
				row, err := cdb.WithTxResult(ctx, session, func(tx *cdb.Tx) (*cdbm.Domain, error) {
					row, _, err := dao.ReserveOwned(ctx, tx, cdbm.DomainCreateInput{
						Hostname: name, Org: "sweep-org", TenantID: &tenant.ID, SiteID: &site.ID,
						ControllerDomainID: &coreID, Status: cdbm.DomainStatusPending, CreatedBy: user.ID,
					})
					return row, err
				})
				require.NoError(t, err)
				rows = append(rows, row)
			}
			// ClaimRecovery orders equal-attempt rows by updated, then ID. Give
			// the cancellation case a deterministic first claim even when the
			// inserts receive the same timestamp.
			for i, row := range rows {
				_, err = session.DB.ExecContext(ctx, `UPDATE domain SET recovery_next_at = current_timestamp - interval '1 second', updated = ? WHERE id = ?`,
					time.Date(2026, time.January, 1, 0, 0, i, 0, time.UTC), row.ID)
				require.NoError(t, err)
			}

			client := &tmocks.Client{}
			pool := sc.NewClientPool(nil)
			pool.IDClientMap[site.ID.String()] = client
			for i, row := range rows {
				if tc.cancelFirst && i > 0 {
					break
				}
				run := &tmocks.WorkflowRun{}
				var resultErr error
				if i == 0 && tc.firstFails {
					resultErr = tp.NewNonRetryableApplicationError("Site unavailable", swe.ErrTypeNICoUnavailable, errors.New("Site unavailable"))
				} else if i == 0 && tc.cancelFirst {
					resultErr = context.Canceled
				}
				run.On("Get", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					requestCtx := args.Get(0).(context.Context)
					deadline, bounded := requestCtx.Deadline()
					require.True(t, bounded)
					require.LessOrEqual(t, time.Until(deadline), 65*time.Second)
					if tc.cancelFirst {
						cancel()
					}
					payload, err := protojson.Marshal(&corev1.Domain{
						Id: &corev1.DomainId{Value: row.ControllerDomainID.String()}, Name: row.Hostname,
					})
					require.NoError(t, err)
					args.Get(1).(*grpcproxy.Response).ResponseJSON = payload
				}).Return(resultErr).Once()
				client.On("ExecuteWorkflow", mock.Anything, mock.Anything, grpcproxy.Core.WorkflowName,
					mock.MatchedBy(func(request grpcproxy.Request) bool {
						var forwarded corev1.CreateDomainRequest
						return request.FullMethod == corev1.Forge_CreateDomain_FullMethodName &&
							protojson.Unmarshal(request.RequestJSON, &forwarded) == nil &&
							forwarded.GetReservedId().GetValue() == row.ControllerDomainID.String()
					})).Return(run, nil).Once()
				t.Cleanup(func() { run.AssertExpectations(t) })
			}

			err = (ManageDomain{DB: session, Sites: pool}).ReconcileReservedDomains(ctx)
			if tc.cancelFirst {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.NoError(t, err)
			}
			for i, row := range rows {
				stored, err := dao.GetByID(context.Background(), nil, row.ID, nil)
				require.NoError(t, err)
				expectedStatus, expectedAttempts := cdbm.DomainStatusReady, 1
				if tc.cancelFirst || (i == 0 && tc.firstFails) {
					expectedStatus = cdbm.DomainStatusPending
				}
				if tc.cancelFirst && i > 0 {
					expectedAttempts = 0
				}
				require.Equal(t, expectedStatus, stored.Status)
				require.Equal(t, expectedAttempts, stored.RecoveryAttempts)
				require.Nil(t, stored.RecoveryToken)
			}
			client.AssertExpectations(t)
		})
	}
}
