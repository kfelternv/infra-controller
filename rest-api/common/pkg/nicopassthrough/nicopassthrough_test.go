// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package nicopassthrough

import "testing"

func TestMethodName(t *testing.T) {
	cases := map[string]string{
		"FindMachineIds":              "FindMachineIds",
		"/forge.Forge/FindMachineIds": "FindMachineIds",
		"forge.Forge/CreateVpc":       "CreateVpc",
		"":                            "",
	}
	for in, want := range cases {
		if got := MethodName(in); got != want {
			t.Errorf("MethodName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFullMethod(t *testing.T) {
	cases := map[string]string{
		"FindMachineIds":              "/forge.Forge/FindMachineIds",
		"/forge.Forge/FindMachineIds": "/forge.Forge/FindMachineIds",
		"CreateVpc":                   "/forge.Forge/CreateVpc",
	}
	for in, want := range cases {
		if got := FullMethod(in); got != want {
			t.Errorf("FullMethod(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsMutation(t *testing.T) {
	reads := []string{
		"Version", "Echo",
		"FindMachineIds", "FindVpcsByIds",
		"GetPowerOptions", "GetComponentInventory",
		"ListMachineHealthReports", "SearchVpcPrefixes",
		"LookupRecord", "IdentifyUuid", "DetermineMachineIngestionState",
		"/forge.Forge/FindMachineIds",
	}
	for _, m := range reads {
		if IsMutation(m) {
			t.Errorf("IsMutation(%q) = true, want false (read method)", m)
		}
	}

	mutations := []string{
		"CreateVpc", "UpdateVpc", "DeleteVpc",
		"AddExpectedMachine", "RemoveStaticAddress",
		"SetPowerShelfMaintenance", "InsertMachineHealthReport",
		"AllocateInstance", "ReleaseInstance",
		"AdminForceDeleteSwitch", "InvokeInstancePower",
		"AssignStaticAddress",
		// Default-deny: an unknown / newly added method is treated as a mutation.
		"SomeBrandNewUnclassifiedMethod",
		"/forge.Forge/CreateVpc",
	}
	for _, m := range mutations {
		if !IsMutation(m) {
			t.Errorf("IsMutation(%q) = false, want true (mutation/default-deny)", m)
		}
	}
}
