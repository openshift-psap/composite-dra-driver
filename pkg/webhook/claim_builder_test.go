package webhook

import (
	"testing"

	resourceapi "k8s.io/api/resource/v1"
)

func TestBuildClaimSpec_Basic(t *testing.T) {
	spec := BuildClaimSpec("composite-gpu-nic", 4)

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

	if len(spec.Devices.Constraints) != 0 {
		t.Fatalf("expected 0 constraints, got %d", len(spec.Devices.Constraints))
	}
}

func TestBuildClaimSpec_SinglePair(t *testing.T) {
	spec := BuildClaimSpec("composite-gpu-nic", 1)

	if len(spec.Devices.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(spec.Devices.Requests))
	}
	if spec.Devices.Requests[0].Name != "pair-0" {
		t.Errorf("name = %s, want pair-0", spec.Devices.Requests[0].Name)
	}
}

func TestBuildClaimSpec_RequestNames(t *testing.T) {
	spec := BuildClaimSpec("composite-gpu-nic", 3)

	expected := []string{"pair-0", "pair-1", "pair-2"}
	for i, req := range spec.Devices.Requests {
		if req.Name != expected[i] {
			t.Errorf("request[%d].Name = %s, want %s", i, req.Name, expected[i])
		}
	}
}

func TestBuildClaimSpec_EightPairs(t *testing.T) {
	spec := BuildClaimSpec("composite-gpu-nic", 8)

	if len(spec.Devices.Requests) != 8 {
		t.Fatalf("expected 8 requests, got %d", len(spec.Devices.Requests))
	}
	if len(spec.Devices.Constraints) != 0 {
		t.Fatalf("expected 0 constraints, got %d", len(spec.Devices.Constraints))
	}
}
