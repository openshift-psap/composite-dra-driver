package synthesizer

import (
	"testing"

	resourceapi "k8s.io/api/resource/v1"

	"github.com/openshift-psap/composite-dra-driver/pkg/config"
)

func strAttr(s string) resourceapi.DeviceAttribute {
	return resourceapi.DeviceAttribute{StringValue: &s}
}

func intAttr(i int64) resourceapi.DeviceAttribute {
	return resourceapi.DeviceAttribute{IntValue: &i}
}

func boolAttr(b bool) resourceapi.DeviceAttribute {
	return resourceapi.DeviceAttribute{BoolValue: &b}
}

func testSources() []config.SourceConfig {
	return []config.SourceConfig{
		{
			Name:           "gpu",
			Driver:         "gpu.nvidia.com",
			DeviceClassName: "gpu.nvidia.com",
			ForwardAttributes: []config.AttributeGroup{
				{Domain: "resource.kubernetes.io", Attributes: []string{"pciBusID", "pcieRoot"}},
			},
		},
		{
			Name:           "nic",
			Driver:         "dra.net",
			DeviceClassName: "dranet",
			ForwardAttributes: []config.AttributeGroup{
				{Domain: "dra.net", Attributes: []string{"pciAddress", "rdma", "ipv4"}},
				{Domain: "resource.kubernetes.io", Attributes: []string{"pcieRoot"}},
			},
		},
	}
}

func testCompositions() []config.CompositionConfig {
	return []config.CompositionConfig{
		{
			Name: "gpu-nic-pair",
			Members: []config.MemberConfig{
				{Source: "gpu", Count: 1},
				{Source: "nic", Count: 1},
			},
			Constraints: []config.ConstraintConfig{
				{Type: "matchAttribute", Attribute: "resource.kubernetes.io/pcieRoot"},
			},
		},
	}
}

func TestPairer_BasicPairing(t *testing.T) {
	pairer := NewPairer(testSources(), testCompositions())

	devices := map[string][]SourceDevice{
		"gpu": {
			{
				SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "gpu-pool", DeviceName: "gpu-0",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"resource.kubernetes.io/pciBusID":  strAttr("0000:0c:00.0"),
					"resource.kubernetes.io/pcieRoot":  strAttr("root-0"),
				},
			},
		},
		"nic": {
			{
				SourceName: "nic", Driver: "dra.net", Pool: "nic-pool", DeviceName: "nic-0",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"dra.net/pciAddress":               strAttr("0000:0d:00.0"),
					"dra.net/rdma":                     boolAttr(true),
					"dra.net/ipv4":                     strAttr("10.0.0.5"),
					"resource.kubernetes.io/pcieRoot":  strAttr("root-0"),
				},
			},
		},
	}

	result := pairer.ComputePairs(devices)
	if len(result) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(result))
	}

	pair := result[0]
	if pair.Mapping == nil {
		t.Fatal("mapping is nil")
	}
	if len(pair.Mapping.Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(pair.Mapping.Members))
	}
	if pair.Mapping.Members[0].Driver != "gpu.nvidia.com" {
		t.Errorf("first member driver = %s, want gpu.nvidia.com", pair.Mapping.Members[0].Driver)
	}
	if pair.Mapping.Members[1].Driver != "dra.net" {
		t.Errorf("second member driver = %s, want dra.net", pair.Mapping.Members[1].Driver)
	}
}

func TestPairer_NoPairingWhenPCIeRootMismatch(t *testing.T) {
	pairer := NewPairer(testSources(), testCompositions())

	devices := map[string][]SourceDevice{
		"gpu": {
			{
				SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "p", DeviceName: "gpu-0",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"resource.kubernetes.io/pcieRoot": strAttr("root-0"),
				},
			},
		},
		"nic": {
			{
				SourceName: "nic", Driver: "dra.net", Pool: "p", DeviceName: "nic-0",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"resource.kubernetes.io/pcieRoot": strAttr("root-1"),
				},
			},
		},
	}

	result := pairer.ComputePairs(devices)
	if len(result) != 0 {
		t.Fatalf("expected 0 pairs (PCIe root mismatch), got %d", len(result))
	}
}

func TestPairer_MultiplePairsOnSameRoot(t *testing.T) {
	pairer := NewPairer(testSources(), testCompositions())

	devices := map[string][]SourceDevice{
		"gpu": {
			{SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "p", DeviceName: "gpu-0",
				Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pcieRoot": strAttr("root-0")}},
			{SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "p", DeviceName: "gpu-1",
				Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pcieRoot": strAttr("root-0")}},
		},
		"nic": {
			{SourceName: "nic", Driver: "dra.net", Pool: "p", DeviceName: "nic-0",
				Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pcieRoot": strAttr("root-0")}},
			{SourceName: "nic", Driver: "dra.net", Pool: "p", DeviceName: "nic-1",
				Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pcieRoot": strAttr("root-0")}},
		},
	}

	result := pairer.ComputePairs(devices)
	// 2 GPUs x 2 NICs on same root = 4 pairs (all combinations)
	if len(result) != 4 {
		t.Fatalf("expected 4 pairs (2x2 on same root), got %d", len(result))
	}
}

func TestPairer_MultipleRoots(t *testing.T) {
	pairer := NewPairer(testSources(), testCompositions())

	devices := map[string][]SourceDevice{
		"gpu": {
			{SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "p", DeviceName: "gpu-0",
				Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pcieRoot": strAttr("root-0")}},
			{SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "p", DeviceName: "gpu-1",
				Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pcieRoot": strAttr("root-1")}},
		},
		"nic": {
			{SourceName: "nic", Driver: "dra.net", Pool: "p", DeviceName: "nic-0",
				Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pcieRoot": strAttr("root-0")}},
			{SourceName: "nic", Driver: "dra.net", Pool: "p", DeviceName: "nic-1",
				Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pcieRoot": strAttr("root-1")}},
		},
	}

	result := pairer.ComputePairs(devices)
	// 1 GPU x 1 NIC per root = 2 pairs
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs (one per root), got %d", len(result))
	}
}

func TestPairer_NoNICs(t *testing.T) {
	pairer := NewPairer(testSources(), testCompositions())

	devices := map[string][]SourceDevice{
		"gpu": {
			{SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "p", DeviceName: "gpu-0",
				Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pcieRoot": strAttr("root-0")}},
		},
	}

	result := pairer.ComputePairs(devices)
	if len(result) != 0 {
		t.Fatalf("expected 0 pairs (no NICs), got %d", len(result))
	}
}

func TestPairer_AttributeForwarding(t *testing.T) {
	pairer := NewPairer(testSources(), testCompositions())

	devices := map[string][]SourceDevice{
		"gpu": {
			{SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "p", DeviceName: "gpu-0",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"resource.kubernetes.io/pciBusID": strAttr("0000:0c:00.0"),
					"resource.kubernetes.io/pcieRoot": strAttr("root-0"),
				}},
		},
		"nic": {
			{SourceName: "nic", Driver: "dra.net", Pool: "p", DeviceName: "nic-0",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"dra.net/pciAddress":              strAttr("0000:0d:00.0"),
					"dra.net/rdma":                    boolAttr(true),
					"dra.net/ipv4":                    strAttr("10.0.0.5"),
					"resource.kubernetes.io/pcieRoot": strAttr("root-0"),
				}},
		},
	}

	result := pairer.ComputePairs(devices)
	if len(result) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(result))
	}

	attrs := result[0].Attributes

	// Check forwarded GPU attributes
	if v, ok := attrs["gpu/pciBusID"]; !ok || *v.StringValue != "0000:0c:00.0" {
		t.Errorf("expected gpu/pciBusID = 0000:0c:00.0, got %v", attrs["gpu/pciBusID"])
	}

	// Check forwarded NIC attributes
	if v, ok := attrs["nic/pciAddress"]; !ok || *v.StringValue != "0000:0d:00.0" {
		t.Errorf("expected nic/pciAddress = 0000:0d:00.0, got %v", attrs["nic/pciAddress"])
	}
	if v, ok := attrs["nic/rdma"]; !ok || *v.BoolValue != true {
		t.Errorf("expected nic/rdma = true, got %v", attrs["nic/rdma"])
	}

	// Check shared constraint attribute (pcieRoot) at top level
	if v, ok := attrs["resource.kubernetes.io/pcieRoot"]; !ok || *v.StringValue != "root-0" {
		t.Errorf("expected pcieRoot = root-0, got %v", attrs["resource.kubernetes.io/pcieRoot"])
	}
}

func TestChooseCombinations(t *testing.T) {
	tests := []struct {
		n, k     int
		expected int
	}{
		{4, 1, 4},
		{4, 2, 6},
		{4, 3, 4},
		{4, 4, 1},
		{3, 2, 3},
		{1, 1, 1},
		{0, 1, 0},
		{3, 0, 0},
	}

	for _, tt := range tests {
		result := chooseCombinations(tt.n, tt.k)
		if len(result) != tt.expected {
			t.Errorf("C(%d,%d) = %d, want %d", tt.n, tt.k, len(result), tt.expected)
		}
	}
}
