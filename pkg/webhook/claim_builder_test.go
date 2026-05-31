package webhook

import (
	"testing"

	resourceapi "k8s.io/api/resource/v1"
)

func TestBuildClaimSpec_BasicNoPairsPerNUMA(t *testing.T) {
	spec := BuildClaimSpec("composite-gpu-nic", "composite/numaNode", 4, 4)

	if len(spec.Devices.Requests) != 4 {
		t.Fatalf("expected 4 requests, got %d", len(spec.Devices.Requests))
	}
	for i, req := range spec.Devices.Requests {
		if req.Exactly == nil {
			t.Fatalf("request %d: Exactly is nil", i)
		}
		if req.Exactly.DeviceClassName != "composite-gpu-nic" {
			t.Errorf("request %d: className = %s, want composite-gpu-nic", i, req.Exactly.DeviceClassName)
		}
		if req.Exactly.Count != 1 {
			t.Errorf("request %d: count = %d, want 1", i, req.Exactly.Count)
		}
		if req.Exactly.AllocationMode != resourceapi.DeviceAllocationModeExactCount {
			t.Errorf("request %d: allocationMode = %v, want ExactCount", i, req.Exactly.AllocationMode)
		}
	}

	// pairsPerNUMA == pairCount → no constraints
	if len(spec.Devices.Constraints) != 0 {
		t.Fatalf("expected 0 constraints when pairsPerNUMA == pairCount, got %d", len(spec.Devices.Constraints))
	}
}

func TestBuildClaimSpec_NUMAConstraints_8_4(t *testing.T) {
	spec := BuildClaimSpec("composite-gpu-nic", "composite/numaNode", 8, 4)

	if len(spec.Devices.Requests) != 8 {
		t.Fatalf("expected 8 requests, got %d", len(spec.Devices.Requests))
	}

	if len(spec.Devices.Constraints) != 2 {
		t.Fatalf("expected 2 constraints (8/4), got %d", len(spec.Devices.Constraints))
	}

	// First constraint: pair-0 through pair-3
	c0 := spec.Devices.Constraints[0]
	if len(c0.Requests) != 4 {
		t.Errorf("constraint[0]: expected 4 requests, got %d", len(c0.Requests))
	}
	if c0.Requests[0] != "pair-0" || c0.Requests[3] != "pair-3" {
		t.Errorf("constraint[0]: requests = %v, want [pair-0..pair-3]", c0.Requests)
	}
	if c0.MatchAttribute == nil || string(*c0.MatchAttribute) != "composite/numaNode" {
		t.Errorf("constraint[0]: matchAttribute = %v, want composite/numaNode", c0.MatchAttribute)
	}

	// Second constraint: pair-4 through pair-7
	c1 := spec.Devices.Constraints[1]
	if len(c1.Requests) != 4 {
		t.Errorf("constraint[1]: expected 4 requests, got %d", len(c1.Requests))
	}
	if c1.Requests[0] != "pair-4" || c1.Requests[3] != "pair-7" {
		t.Errorf("constraint[1]: requests = %v, want [pair-4..pair-7]", c1.Requests)
	}
}

func TestBuildClaimSpec_NUMAConstraints_6_4(t *testing.T) {
	spec := BuildClaimSpec("composite-gpu-nic", "composite/numaNode", 6, 4)

	if len(spec.Devices.Constraints) != 2 {
		t.Fatalf("expected 2 constraints (6/4), got %d", len(spec.Devices.Constraints))
	}

	// First group: 4 pairs
	if len(spec.Devices.Constraints[0].Requests) != 4 {
		t.Errorf("constraint[0]: expected 4 requests, got %d", len(spec.Devices.Constraints[0].Requests))
	}
	// Second group: 2 pairs (remainder)
	if len(spec.Devices.Constraints[1].Requests) != 2 {
		t.Errorf("constraint[1]: expected 2 requests, got %d", len(spec.Devices.Constraints[1].Requests))
	}
}

func TestBuildClaimSpec_NoNUMA(t *testing.T) {
	spec := BuildClaimSpec("composite-gpu-nic", "", 4, 2)

	if len(spec.Devices.Requests) != 4 {
		t.Fatalf("expected 4 requests, got %d", len(spec.Devices.Requests))
	}
	// Empty numaAttribute → no constraints regardless of pairsPerNUMA
	if len(spec.Devices.Constraints) != 0 {
		t.Fatalf("expected 0 constraints with empty NUMA attribute, got %d", len(spec.Devices.Constraints))
	}
}

func TestBuildClaimSpec_SinglePair(t *testing.T) {
	spec := BuildClaimSpec("composite-gpu-nic", "composite/numaNode", 1, 1)

	if len(spec.Devices.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(spec.Devices.Requests))
	}
	if len(spec.Devices.Constraints) != 0 {
		t.Fatalf("expected 0 constraints for single pair, got %d", len(spec.Devices.Constraints))
	}
}

func TestBuildClaimSpec_RequestNames(t *testing.T) {
	spec := BuildClaimSpec("composite-gpu-nic", "", 3, 3)

	expected := []string{"pair-0", "pair-1", "pair-2"}
	for i, req := range spec.Devices.Requests {
		if req.Name != expected[i] {
			t.Errorf("request[%d].Name = %s, want %s", i, req.Name, expected[i])
		}
	}
}
