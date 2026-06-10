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
	pairer := NewPairer(testSources(), testCompositions(), nil)

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
	pairer := NewPairer(testSources(), testCompositions(), nil)

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
	pairer := NewPairer(testSources(), testCompositions(), nil)

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
	pairer := NewPairer(testSources(), testCompositions(), nil)

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
	pairer := NewPairer(testSources(), testCompositions(), nil)

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
	pairer := NewPairer(testSources(), testCompositions(), nil)

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

	// Check compositionName attribute
	if v, ok := attrs["composite/compositionName"]; !ok || *v.StringValue != "gpu-nic-pair" {
		t.Errorf("expected composite/compositionName = gpu-nic-pair, got %v", attrs["composite/compositionName"])
	}
}

func TestPairer_MultipleCompositions(t *testing.T) {
	sources := []config.SourceConfig{
		{
			Name: "gpu", Driver: "gpu.nvidia.com", DeviceClassName: "gpu.nvidia.com",
			ForwardAttributes: []config.AttributeGroup{
				{Domain: "resource.kubernetes.io", Attributes: []string{"pcieRoot"}},
			},
		},
		{
			Name: "nic", Driver: "dra.net", DeviceClassName: "dranet",
			ForwardAttributes: []config.AttributeGroup{
				{Domain: "resource.kubernetes.io", Attributes: []string{"pcieRoot"}},
			},
		},
		{
			Name: "fpga", Driver: "fpga.example.io", DeviceClassName: "fpga",
			ForwardAttributes: []config.AttributeGroup{
				{Domain: "resource.kubernetes.io", Attributes: []string{"pcieRoot"}},
			},
		},
	}
	compositions := []config.CompositionConfig{
		{
			Name:    "gpu-nic-pair",
			Members: []config.MemberConfig{{Source: "gpu", Count: 1}, {Source: "nic", Count: 1}},
			Constraints: []config.ConstraintConfig{
				{Type: "matchAttribute", Attribute: "resource.kubernetes.io/pcieRoot"},
			},
		},
		{
			Name:    "gpu-fpga-pair",
			Members: []config.MemberConfig{{Source: "gpu", Count: 1}, {Source: "fpga", Count: 1}},
			Constraints: []config.ConstraintConfig{
				{Type: "matchAttribute", Attribute: "resource.kubernetes.io/pcieRoot"},
			},
		},
	}

	pairer := NewPairer(sources, compositions)

	devices := map[string][]SourceDevice{
		"gpu": {
			{SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "p", DeviceName: "gpu-0",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"resource.kubernetes.io/pcieRoot": strAttr("root-0"),
				}},
		},
		"nic": {
			{SourceName: "nic", Driver: "dra.net", Pool: "p", DeviceName: "nic-0",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"resource.kubernetes.io/pcieRoot": strAttr("root-0"),
				}},
		},
		"fpga": {
			{SourceName: "fpga", Driver: "fpga.example.io", Pool: "p", DeviceName: "fpga-0",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"resource.kubernetes.io/pcieRoot": strAttr("root-0"),
				}},
		},
	}

	result := pairer.ComputePairs(devices)
	if len(result) != 2 {
		t.Fatalf("expected 2 composite devices (one per composition), got %d", len(result))
	}

	compNames := map[string]bool{}
	for _, cd := range result {
		v, ok := cd.Attributes["composite/compositionName"]
		if !ok || v.StringValue == nil {
			t.Fatal("composite/compositionName attribute missing")
		}
		compNames[*v.StringValue] = true
		if cd.Mapping.CompositionName != *v.StringValue {
			t.Errorf("mapping.CompositionName=%s != attribute=%s", cd.Mapping.CompositionName, *v.StringValue)
		}
	}

	if !compNames["gpu-nic-pair"] {
		t.Error("missing gpu-nic-pair composition")
	}
	if !compNames["gpu-fpga-pair"] {
		t.Error("missing gpu-fpga-pair composition")
	}
}

func TestPairer_SingleMemberComposition(t *testing.T) {
	sources := []config.SourceConfig{
		{
			Name: "gpu", Driver: "gpu.nvidia.com", DeviceClassName: "gpu.nvidia.com",
			ForwardAttributes: []config.AttributeGroup{
				{Domain: "gpu.nvidia.com", Attributes: []string{"model"}},
			},
		},
	}
	compositions := []config.CompositionConfig{
		{
			Name:    "gpu",
			Members: []config.MemberConfig{{Source: "gpu", Count: 1}},
		},
	}

	pairer := NewPairer(sources, compositions)

	devices := map[string][]SourceDevice{
		"gpu": {
			{SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "p", DeviceName: "gpu-0",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"gpu.nvidia.com/model": strAttr("A100"),
				}},
			{SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "p", DeviceName: "gpu-1",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"gpu.nvidia.com/model": strAttr("A100"),
				}},
		},
	}

	result := pairer.ComputePairs(devices)
	if len(result) != 2 {
		t.Fatalf("expected 2 composite devices (one per GPU), got %d", len(result))
	}

	for _, cd := range result {
		if v, ok := cd.Attributes["composite/compositionName"]; !ok || *v.StringValue != "gpu" {
			t.Errorf("expected compositionName=gpu, got %v", cd.Attributes["composite/compositionName"])
		}
		if len(cd.Mapping.Members) != 1 {
			t.Errorf("expected 1 member, got %d", len(cd.Mapping.Members))
		}
		if cd.Mapping.Members[0].Driver != "gpu.nvidia.com" {
			t.Errorf("member driver = %s, want gpu.nvidia.com", cd.Mapping.Members[0].Driver)
		}
		if v, ok := cd.Attributes["gpu/model"]; !ok || *v.StringValue != "A100" {
			t.Errorf("expected forwarded gpu/model=A100, got %v", cd.Attributes["gpu/model"])
		}
	}
}

func explicitCompositions(pools []config.ExplicitNodePool) []config.CompositionConfig {
	return []config.CompositionConfig{
		{
			Name:             "gpu-nic-pair",
			PairingMode:      "explicit",
			NodePoolLabelKey: "node.kubernetes.io/instance-type",
			Members: []config.MemberConfig{
				{Source: "gpu", Count: 1},
				{Source: "nic", Count: 1},
			},
			NodePools: pools,
		},
	}
}

func testExplicitDevices() map[string][]SourceDevice {
	return map[string][]SourceDevice{
		"gpu": {
			{SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "p", DeviceName: "gpu-0",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"resource.kubernetes.io/pciBusID": strAttr("0000:0c:00.0"),
					"resource.kubernetes.io/pcieRoot": strAttr("root-0"),
				}},
			{SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "p", DeviceName: "gpu-1",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"resource.kubernetes.io/pciBusID": strAttr("0000:43:00.0"),
					"resource.kubernetes.io/pcieRoot": strAttr("root-1"),
				}},
		},
		"nic": {
			{SourceName: "nic", Driver: "dra.net", Pool: "p", DeviceName: "nic-0",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"dra.net/pciAddress": strAttr("0000:0d:00.0"),
					"dra.net/rdma":       boolAttr(true),
				}},
			{SourceName: "nic", Driver: "dra.net", Pool: "p", DeviceName: "nic-1",
				Attributes: map[string]resourceapi.DeviceAttribute{
					"dra.net/pciAddress": strAttr("0000:44:00.0"),
					"dra.net/rdma":       boolAttr(true),
				}},
		},
	}
}

func TestPairer_ExplicitPairing_Basic(t *testing.T) {
	pools := []config.ExplicitNodePool{
		{
			Label: "gpu-h100",
			Pairs: []config.ExplicitPairConfig{
				{
					Selectors: map[string]string{
						"gpu": `device.attributes["resource.kubernetes.io"].pciBusID == "0000:0c:00.0"`,
						"nic": `device.attributes["dra.net"].pciAddress == "0000:0d:00.0"`,
					},
					Rail: 0, NUMA: 0,
				},
			},
		},
	}
	nodeLabels := map[string]string{"node.kubernetes.io/instance-type": "gpu-h100"}
	pairer := NewPairer(testSources(), explicitCompositions(pools), nodeLabels)

	result := pairer.ComputePairs(testExplicitDevices())
	if len(result) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(result))
	}
	if result[0].Mapping.Members[0].Device != "gpu-0" {
		t.Errorf("expected gpu-0, got %s", result[0].Mapping.Members[0].Device)
	}
	if result[0].Mapping.Members[1].Device != "nic-0" {
		t.Errorf("expected nic-0, got %s", result[0].Mapping.Members[1].Device)
	}
}

func TestPairer_ExplicitPairing_NoMatch(t *testing.T) {
	pools := []config.ExplicitNodePool{
		{
			Label: "gpu-h100",
			Pairs: []config.ExplicitPairConfig{
				{
					Selectors: map[string]string{
						"gpu": `device.attributes["resource.kubernetes.io"].pciBusID == "9999:99:99.0"`,
						"nic": `device.attributes["dra.net"].pciAddress == "0000:0d:00.0"`,
					},
				},
			},
		},
	}
	nodeLabels := map[string]string{"node.kubernetes.io/instance-type": "gpu-h100"}
	pairer := NewPairer(testSources(), explicitCompositions(pools), nodeLabels)

	result := pairer.ComputePairs(testExplicitDevices())
	if len(result) != 0 {
		t.Fatalf("expected 0 pairs (GPU selector no match), got %d", len(result))
	}
}

func TestPairer_ExplicitPairing_NoMatchingPool(t *testing.T) {
	pools := []config.ExplicitNodePool{
		{
			Label: "gpu-h100",
			Pairs: []config.ExplicitPairConfig{
				{
					Selectors: map[string]string{
						"gpu": `device.attributes["resource.kubernetes.io"].pciBusID == "0000:0c:00.0"`,
						"nic": `device.attributes["dra.net"].pciAddress == "0000:0d:00.0"`,
					},
				},
			},
		},
	}
	nodeLabels := map[string]string{"node.kubernetes.io/instance-type": "gpu-a100"}
	pairer := NewPairer(testSources(), explicitCompositions(pools), nodeLabels)

	result := pairer.ComputePairs(testExplicitDevices())
	if len(result) != 0 {
		t.Fatalf("expected 0 pairs (no matching pool), got %d", len(result))
	}
}

func TestPairer_ExplicitPairing_MultiplePairs(t *testing.T) {
	pools := []config.ExplicitNodePool{
		{
			Label: "gpu-h100",
			Pairs: []config.ExplicitPairConfig{
				{
					Selectors: map[string]string{
						"gpu": `device.attributes["resource.kubernetes.io"].pciBusID == "0000:0c:00.0"`,
						"nic": `device.attributes["dra.net"].pciAddress == "0000:0d:00.0"`,
					},
					Rail: 0, NUMA: 0,
				},
				{
					Selectors: map[string]string{
						"gpu": `device.attributes["resource.kubernetes.io"].pciBusID == "0000:43:00.0"`,
						"nic": `device.attributes["dra.net"].pciAddress == "0000:44:00.0"`,
					},
					Rail: 1, NUMA: 1,
				},
			},
		},
	}
	nodeLabels := map[string]string{"node.kubernetes.io/instance-type": "gpu-h100"}
	pairer := NewPairer(testSources(), explicitCompositions(pools), nodeLabels)

	result := pairer.ComputePairs(testExplicitDevices())
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(result))
	}
}

func TestPairer_ExplicitPairing_DeviceConsumed(t *testing.T) {
	pools := []config.ExplicitNodePool{
		{
			Label: "gpu-h100",
			Pairs: []config.ExplicitPairConfig{
				{
					Selectors: map[string]string{
						"gpu": `device.attributes["resource.kubernetes.io"].pciBusID == "0000:0c:00.0"`,
						"nic": `device.attributes["dra.net"].rdma == true`,
					},
					Rail: 0, NUMA: 0,
				},
				{
					Selectors: map[string]string{
						"gpu": `device.attributes["resource.kubernetes.io"].pciBusID == "0000:43:00.0"`,
						"nic": `device.attributes["dra.net"].rdma == true`,
					},
					Rail: 1, NUMA: 1,
				},
			},
		},
	}
	nodeLabels := map[string]string{"node.kubernetes.io/instance-type": "gpu-h100"}
	pairer := NewPairer(testSources(), explicitCompositions(pools), nodeLabels)

	result := pairer.ComputePairs(testExplicitDevices())
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(result))
	}
	nic0 := result[0].Mapping.Members[1].Device
	nic1 := result[1].Mapping.Members[1].Device
	if nic0 == nic1 {
		t.Errorf("same NIC used in both pairs: %s", nic0)
	}
}

func TestPairer_ExplicitPairing_RailAndNUMA(t *testing.T) {
	pools := []config.ExplicitNodePool{
		{
			Label: "gpu-h100",
			Pairs: []config.ExplicitPairConfig{
				{
					Selectors: map[string]string{
						"gpu": `device.attributes["resource.kubernetes.io"].pciBusID == "0000:0c:00.0"`,
						"nic": `device.attributes["dra.net"].pciAddress == "0000:0d:00.0"`,
					},
					Rail: 3, NUMA: 2,
				},
			},
		},
	}
	nodeLabels := map[string]string{"node.kubernetes.io/instance-type": "gpu-h100"}
	pairer := NewPairer(testSources(), explicitCompositions(pools), nodeLabels)

	result := pairer.ComputePairs(testExplicitDevices())
	if len(result) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(result))
	}
	if result[0].Mapping.RailIndex != 3 {
		t.Errorf("expected RailIndex=3, got %d", result[0].Mapping.RailIndex)
	}
	if result[0].Mapping.NUMANode != 2 {
		t.Errorf("expected NUMANode=2, got %d", result[0].Mapping.NUMANode)
	}
	if v, ok := result[0].Attributes["composite/railIndex"]; !ok || *v.IntValue != 3 {
		t.Errorf("expected composite/railIndex=3, got %v", result[0].Attributes["composite/railIndex"])
	}
	if v, ok := result[0].Attributes["composite/numaNode"]; !ok || *v.IntValue != 2 {
		t.Errorf("expected composite/numaNode=2, got %v", result[0].Attributes["composite/numaNode"])
	}
}

func TestPairer_ExplicitPairing_WithFilter(t *testing.T) {
	pools := []config.ExplicitNodePool{
		{
			Label: "gpu-h100",
			Pairs: []config.ExplicitPairConfig{
				{
					Selectors: map[string]string{
						"gpu": `device.attributes["resource.kubernetes.io"].pciBusID == "0000:0c:00.0"`,
						"nic": `device.attributes["dra.net"].pciAddress == "0000:0d:00.0"`,
					},
				},
			},
		},
	}
	comps := explicitCompositions(pools)
	comps[0].Filters = map[string]config.FilterConfig{
		"nic": {CEL: `device.attributes["dra.net"].rdma == false`},
	}
	nodeLabels := map[string]string{"node.kubernetes.io/instance-type": "gpu-h100"}
	pairer := NewPairer(testSources(), comps, nodeLabels)

	result := pairer.ComputePairs(testExplicitDevices())
	if len(result) != 0 {
		t.Fatalf("expected 0 pairs (filter excludes all NICs), got %d", len(result))
	}
}

func TestPairer_ExplicitPairing_AttributeForwarding(t *testing.T) {
	pools := []config.ExplicitNodePool{
		{
			Label: "gpu-h100",
			Pairs: []config.ExplicitPairConfig{
				{
					Selectors: map[string]string{
						"gpu": `device.attributes["resource.kubernetes.io"].pciBusID == "0000:0c:00.0"`,
						"nic": `device.attributes["dra.net"].pciAddress == "0000:0d:00.0"`,
					},
				},
			},
		},
	}
	nodeLabels := map[string]string{"node.kubernetes.io/instance-type": "gpu-h100"}
	pairer := NewPairer(testSources(), explicitCompositions(pools), nodeLabels)

	result := pairer.ComputePairs(testExplicitDevices())
	if len(result) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(result))
	}
	attrs := result[0].Attributes
	if v, ok := attrs["gpu/pciBusID"]; !ok || *v.StringValue != "0000:0c:00.0" {
		t.Errorf("expected gpu/pciBusID forwarded")
	}
	if v, ok := attrs["nic/pciAddress"]; !ok || *v.StringValue != "0000:0d:00.0" {
		t.Errorf("expected nic/pciAddress forwarded")
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
