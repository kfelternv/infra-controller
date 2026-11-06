// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package subnet

import (
	"context"
	"testing"

	cutil "github.com/NVIDIA/infra-controller/rest-api/common/pkg/util"
	cdb "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db"
	"github.com/NVIDIA/infra-controller/rest-api/db/pkg/db/ipam"
	cdbm "github.com/NVIDIA/infra-controller/rest-api/db/pkg/db/model"
	cipam "github.com/NVIDIA/infra-controller/rest-api/ipam"
	corev1 "github.com/NVIDIA/infra-controller/rest-api/proto/core/gen/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func testSubnetInventoryDomainIdentity(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mapping   string
		fullGrant bool
	}{
		{name: "no_domain_control", mapping: "none"},
		{name: "owned_domain_with_distinct_rest_and_core_ids", mapping: "owned"},
		{name: "missing_mapping_defers_recovery", mapping: "missing"},
		{name: "other_site_mapping_is_not_adopted", mapping: "other_site"},
		{name: "other_tenant_mapping_is_not_adopted", mapping: "other_tenant"},
		{name: "unmanaged_mapping_is_not_adopted", mapping: "unmanaged"},
		{name: "pending_mapping_defers_recovery", mapping: "pending"},
		{name: "deleting_mapping_is_not_adopted", mapping: "deleting"},
		{name: "deleted_mapping_is_not_adopted", mapping: "deleted"},
		{name: "ambiguous_mapping_defers_recovery", mapping: "ambiguous"},
		{name: "missing_mapping_does_not_claim_full_ip_block", mapping: "missing", fullGrant: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dbSession := testSubnetInitDB(t)
			defer dbSession.Close()
			for _, model := range []any{
				(*cdbm.InfrastructureProvider)(nil), (*cdbm.Site)(nil),
				(*cdbm.Tenant)(nil), (*cdbm.User)(nil), (*cdbm.Allocation)(nil),
				(*cdbm.StatusDetail)(nil), (*cdbm.Domain)(nil), (*cdbm.IPBlock)(nil),
				(*cdbm.NVLinkLogicalPartition)(nil), (*cdbm.NetworkSecurityGroup)(nil),
				(*cdbm.Vpc)(nil), (*cdbm.Subnet)(nil),
			} {
				require.NoError(t, dbSession.DB.ResetModel(ctx, model))
			}
			ipamSchema := cipam.NewBunStorage(dbSession.DB, nil)
			require.NoError(t, ipamSchema.ApplyDbSchema())
			require.NoError(t, ipamSchema.DeleteAllPrefixes(ctx, ""))
			providerOrg := "inventory-domain-provider"
			providerUser := testSubnetBuildUser(t, dbSession, uuid.NewString(), providerOrg, []string{"FORGE_PROVIDER_ADMIN"})
			provider := testSubnetSiteBuildInfrastructureProvider(t, dbSession, "provider", providerOrg, providerUser)
			site := testSubnetBuildSite(t, dbSession, provider, "site", providerUser)
			tenantOrg := "inventory-domain-tenant"
			tenantUser := testSubnetBuildUser(t, dbSession, uuid.NewString(), tenantOrg, []string{"FORGE_TENANT_ADMIN"})
			tenant := testSubnetBuildTenant(t, dbSession, "tenant", tenantOrg, tenantUser)
			vpc := testSubnetBuildVPC(t, dbSession, "vpc", provider, tenant, site, nil, nil, tenantUser)
			blockPrefix, blockLength := "10.87.0.0", 16
			if tc.fullGrant {
				blockPrefix, blockLength = "10.87.2.0", 24
			}
			ipBlock := testSubnetBuildIPBlock(t, dbSession, "block", site, provider, &tenant.ID,
				cdbm.IPBlockRoutingTypeDatacenterOnly, blockPrefix, blockLength,
				cdbm.IPBlockProtocolVersionV4, false, cdbm.IPBlockStatusReady, tenantUser)
			storage := ipam.NewIpamStorage(dbSession.DB, nil)
			_, err := ipam.CreateIpamEntryForIPBlock(ctx, storage, ipBlock.Prefix, ipBlock.PrefixLength,
				ipBlock.RoutingType, ipBlock.InfrastructureProviderID.String(), ipBlock.SiteID.String())
			require.NoError(t, err)

			domainDAO := cdbm.NewDomainDAO(dbSession)
			coreID := uuid.New()
			var domain *cdbm.Domain
			var coreDomainID *corev1.DomainId
			var initialDomains []*cdbm.Domain
			if tc.mapping != "none" {
				coreDomainID = &corev1.DomainId{Value: coreID.String()}
			}
			if tc.mapping != "none" && tc.mapping != "missing" {
				tenantID, siteID := &tenant.ID, &site.ID
				domainOrg := tenantOrg
				status := cdbm.DomainStatusReady
				switch tc.mapping {
				case "other_site":
					other := testSubnetBuildSite(t, dbSession, provider, "other-site", providerUser)
					siteID = &other.ID
				case "other_tenant":
					other := testSubnetBuildTenant(t, dbSession, "other-tenant", "other-tenant-org", tenantUser)
					tenantID = &other.ID
					domainOrg = other.Org
				case "unmanaged":
					tenantID, siteID = nil, nil
				case "pending":
					status = cdbm.DomainStatusPending
				case "deleting":
					status = cdbm.DomainStatusDeleting
				}
				domain, err = domainDAO.Create(ctx, nil, cdbm.DomainCreateInput{
					Hostname: "inventory.example", Org: domainOrg, TenantID: tenantID,
					SiteID: siteID, ControllerDomainID: &coreID, Status: status, CreatedBy: tenantUser.ID,
				})
				require.NoError(t, err)
				require.NotEqual(t, domain.ID, coreID)
				initialDomains = append(initialDomains, domain)
				if tc.mapping == "deleted" {
					require.NoError(t, domainDAO.Delete(ctx, nil, domain.ID))
				}
				if tc.mapping == "ambiguous" {
					other, createErr := domainDAO.Create(ctx, nil, cdbm.DomainCreateInput{
						Hostname: "ambiguous.example", Org: tenantOrg, TenantID: tenantID,
						SiteID: siteID, ControllerDomainID: &coreID, Status: status, CreatedBy: tenantUser.ID,
					})
					require.NoError(t, createErr)
					initialDomains = append(initialDomains, other)
				}
			}
			segmentID := uuid.New()
			segment := &corev1.NetworkSegment{
				Id: &corev1.NetworkSegmentId{Value: segmentID.String()},
				Config: &corev1.NetworkSegmentConfig{
					VpcId: &corev1.VpcId{Value: vpc.ID.String()}, SegmentType: corev1.NetworkSegmentType_TENANT,
					SubdomainId: coreDomainID,
					Prefixes:    []*corev1.NetworkPrefix{{Prefix: "10.87.2.0/24", Gateway: cutil.GetPtr("10.87.2.1")}},
				},
				Metadata: &corev1.Metadata{Name: "inventory-recovered"},
			}
			manager := ManageSubnet{dbSession: dbSession}
			recovered := manager.createOrUpdateSubnetFromSite(ctx, site, segment)
			if tc.mapping != "none" && tc.mapping != "owned" {
				require.Nil(t, recovered, "an unowned, missing or ambiguous Domain must not become a REST relation")
				_, lookupErr := cdbm.NewSubnetDAO(dbSession).GetByID(ctx, nil, segmentID, nil)
				require.ErrorIs(t, lookupErr, cdb.ErrDoesNotExist)
				storedBlock, lookupErr := cdbm.NewIPBlockDAO(dbSession).GetByID(ctx, nil, ipBlock.ID, nil)
				require.NoError(t, lookupErr)
				require.False(t, storedBlock.FullGrant, "deferred recovery must not commit an IPAM reservation")
				if tc.mapping == "ambiguous" {
					for _, initial := range initialDomains {
						require.NoError(t, domainDAO.Delete(ctx, nil, initial.ID))
					}
				}
				// A later inventory can recover the same segment/CIDR once its
				// owned mapping is available; failed attempts must not reserve it.
				domain, err = domainDAO.Create(ctx, nil, cdbm.DomainCreateInput{
					Hostname: "ready.example", Org: tenantOrg, TenantID: &tenant.ID, SiteID: &site.ID,
					ControllerDomainID: &coreID, Status: cdbm.DomainStatusReady, CreatedBy: tenantUser.ID,
				})
				require.NoError(t, err)
				recovered = manager.createOrUpdateSubnetFromSite(ctx, site, segment)
			}
			require.NotNil(t, recovered, "inventory must recover the segment using the local REST Domain identity")
			stored, err := cdbm.NewSubnetDAO(dbSession).GetByID(ctx, nil, segmentID, nil)
			require.NoError(t, err)
			require.Equal(t, tenant.ID, stored.TenantID)
			require.Equal(t, site.ID, stored.SiteID)
			require.Equal(t, &segmentID, stored.ControllerNetworkSegmentID)
			require.Equal(t, cutil.GetPtr("10.87.2.0"), stored.IPv4Prefix)
			require.Equal(t, 24, stored.PrefixLength)
			if tc.mapping == "none" {
				require.Nil(t, stored.DomainID)
			} else {
				require.Equal(t, &domain.ID, stored.DomainID)
				require.NotEqual(t, &coreID, stored.DomainID)
			}
			repeated := manager.createOrUpdateSubnetFromSite(ctx, site, segment)
			require.NotNil(t, repeated)
			require.Equal(t, stored.ID, repeated.ID)
			require.Equal(t, stored.DomainID, repeated.DomainID)
		})
	}
}
