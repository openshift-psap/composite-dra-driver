package config

import (
	"testing"
)

func validConfig() *CompositeConfig {
	return &CompositeConfig{
		Driver: DriverConfig{Name: "composite.dra.example.io"},
		Sources: []SourceConfig{
			{Name: "gpu", Driver: "gpu.nvidia.com", DeviceClassName: "gpu.nvidia.com"},
			{Name: "nic", Driver: "dra.net", DeviceClassName: "dranet"},
		},
		Compositions: []CompositionConfig{
			{
				Name: "gpu-nic-pair",
				Members: []MemberConfig{
					{Source: "gpu", Count: 1},
					{Source: "nic", Count: 1},
				},
				Constraints: []ConstraintConfig{
					{Type: "matchAttribute", Attribute: "resource.kubernetes.io/pcieRoot"},
				},
			},
		},
	}
}

func TestValidate_Valid(t *testing.T) {
	if err := Validate(validConfig()); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestValidate_MissingDriverName(t *testing.T) {
	cfg := validConfig()
	cfg.Driver.Name = ""
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for missing driver name")
	}
}

func TestValidate_NoSources(t *testing.T) {
	cfg := validConfig()
	cfg.Sources = nil
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for no sources")
	}
}

func TestValidate_DuplicateSourceName(t *testing.T) {
	cfg := validConfig()
	cfg.Sources = append(cfg.Sources, SourceConfig{Name: "gpu", Driver: "other", DeviceClassName: "other"})
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for duplicate source name")
	}
}

func TestValidate_UnknownSourceInMember(t *testing.T) {
	cfg := validConfig()
	cfg.Compositions[0].Members[0].Source = "nonexistent"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for unknown source in member")
	}
}

func TestValidate_SingleMember(t *testing.T) {
	cfg := validConfig()
	cfg.Compositions[0].Members = cfg.Compositions[0].Members[:1]
	if err := Validate(cfg); err != nil {
		t.Fatalf("single-member composition should be valid, got: %v", err)
	}
}

func TestValidate_ZeroMembers(t *testing.T) {
	cfg := validConfig()
	cfg.Compositions[0].Members = nil
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for zero members")
	}
}

func TestValidate_ZeroMemberCount(t *testing.T) {
	cfg := validConfig()
	cfg.Compositions[0].Members[0].Count = 0
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for zero member count")
	}
}

func TestValidate_SelfReferencingSource(t *testing.T) {
	cfg := validConfig()
	cfg.Sources[0].Driver = cfg.Driver.Name
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for source referencing composite driver itself")
	}
}

func TestValidate_DuplicateCompositionName(t *testing.T) {
	cfg := validConfig()
	cfg.Sources = append(cfg.Sources, SourceConfig{Name: "fpga", Driver: "fpga.io", DeviceClassName: "fpga"})
	cfg.Compositions = append(cfg.Compositions, CompositionConfig{
		Name:    "gpu-nic-pair",
		Members: []MemberConfig{{Source: "gpu", Count: 1}, {Source: "fpga", Count: 1}},
	})
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for duplicate composition name")
	}
}

func TestValidate_DuplicateDeviceClassName(t *testing.T) {
	cfg := validConfig()
	cfg.Sources = append(cfg.Sources, SourceConfig{Name: "fpga", Driver: "fpga.io", DeviceClassName: "fpga"})
	cfg.Compositions = append(cfg.Compositions, CompositionConfig{
		Name:            "gpu-fpga-pair",
		DeviceClassName: "composite-gpu-nic-pair",
		Members:         []MemberConfig{{Source: "gpu", Count: 1}, {Source: "fpga", Count: 1}},
	})
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for duplicate effective deviceClassName")
	}
}

func TestValidate_CustomDeviceClassName(t *testing.T) {
	cfg := validConfig()
	cfg.Compositions[0].DeviceClassName = "my-custom-class"
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid config with custom deviceClassName, got: %v", err)
	}
	if cfg.Compositions[0].EffectiveDeviceClassName() != "my-custom-class" {
		t.Errorf("expected my-custom-class, got %s", cfg.Compositions[0].EffectiveDeviceClassName())
	}
}

func TestCompositionConfig_EffectiveDefaults(t *testing.T) {
	c := CompositionConfig{Name: "gpu-nic-pair"}
	if c.EffectiveDeviceClassName() != "composite-gpu-nic-pair" {
		t.Errorf("default deviceClassName = %s, want composite-gpu-nic-pair", c.EffectiveDeviceClassName())
	}
	if c.EffectiveExtendedResourceName("composite.dra.io") != "composite.dra.io/gpu-nic-pair" {
		t.Errorf("default extendedResourceName = %s", c.EffectiveExtendedResourceName("composite.dra.io"))
	}
}

func TestValidate_UnsupportedConstraintType(t *testing.T) {
	cfg := validConfig()
	cfg.Compositions[0].Constraints[0].Type = "distinctAttribute"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for unsupported constraint type")
	}
}

func TestValidate_InvalidPairingMode(t *testing.T) {
	cfg := validConfig()
	cfg.Compositions[0].PairingMode = "magic"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for invalid pairing mode")
	}
}

func explicitConfig() *CompositeConfig {
	cfg := validConfig()
	cfg.Compositions[0].PairingMode = "explicit"
	cfg.Compositions[0].Constraints = nil
	cfg.Compositions[0].NodePoolLabelKey = "node.kubernetes.io/instance-type"
	cfg.Compositions[0].NodePools = []ExplicitNodePool{
		{
			Label: "gpu-h100",
			Pairs: []ExplicitPairConfig{
				{
					Selectors: map[string]string{
						"gpu": `device.attributes["resource.kubernetes.io"].pciBusID == "0000:0c:00.0"`,
						"nic": `device.attributes["dra.net"].pciAddress == "0000:0d:00.0"`,
					},
					Rail: 0,
					NUMA: 0,
				},
			},
		},
	}
	return cfg
}

func TestValidate_ExplicitMode_Valid(t *testing.T) {
	if err := Validate(explicitConfig()); err != nil {
		t.Fatalf("expected valid explicit config, got: %v", err)
	}
}

func TestValidate_ExplicitMode_MissingNodePools(t *testing.T) {
	cfg := explicitConfig()
	cfg.Compositions[0].NodePools = nil
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for missing nodePools")
	}
}

func TestValidate_ExplicitMode_MissingNodePoolLabelKey(t *testing.T) {
	cfg := explicitConfig()
	cfg.Compositions[0].NodePoolLabelKey = ""
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for missing nodePoolLabelKey")
	}
}

func TestValidate_ExplicitMode_WithConstraints(t *testing.T) {
	cfg := explicitConfig()
	cfg.Compositions[0].Constraints = []ConstraintConfig{
		{Type: "matchAttribute", Attribute: "resource.kubernetes.io/pcieRoot"},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for constraints with explicit mode")
	}
}

func TestValidate_ExplicitMode_UnknownSourceInSelector(t *testing.T) {
	cfg := explicitConfig()
	cfg.Compositions[0].NodePools[0].Pairs[0].Selectors["unknown"] = "true"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for unknown source in selector")
	}
}

func TestValidate_ExplicitMode_MissingSourceInSelector(t *testing.T) {
	cfg := explicitConfig()
	delete(cfg.Compositions[0].NodePools[0].Pairs[0].Selectors, "gpu")
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for missing source in selector")
	}
}

func TestValidate_ExplicitMode_EmptyCEL(t *testing.T) {
	cfg := explicitConfig()
	cfg.Compositions[0].NodePools[0].Pairs[0].Selectors["gpu"] = ""
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for empty CEL expression")
	}
}

func TestValidate_ExplicitMode_NegativeRail(t *testing.T) {
	cfg := explicitConfig()
	cfg.Compositions[0].NodePools[0].Pairs[0].Rail = -1
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for negative rail")
	}
}

func TestValidate_ExplicitMode_NegativeNUMA(t *testing.T) {
	cfg := explicitConfig()
	cfg.Compositions[0].NodePools[0].Pairs[0].NUMA = -1
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for negative NUMA")
	}
}

func TestValidate_AutoMode_WithNodePools(t *testing.T) {
	cfg := validConfig()
	cfg.Compositions[0].NodePools = []ExplicitNodePool{
		{Label: "test", Pairs: []ExplicitPairConfig{{Selectors: map[string]string{"gpu": "true", "nic": "true"}}}},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for nodePools without explicit mode")
	}
}

func TestValidate_ExplicitMode_DuplicatePoolLabel(t *testing.T) {
	cfg := explicitConfig()
	cfg.Compositions[0].NodePools = append(cfg.Compositions[0].NodePools, ExplicitNodePool{
		Label: "gpu-h100",
		Pairs: []ExplicitPairConfig{{Selectors: map[string]string{"gpu": "true", "nic": "true"}}},
	})
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for duplicate pool label")
	}
}

func TestValidate_ExplicitMode_EmptyPoolLabel(t *testing.T) {
	cfg := explicitConfig()
	cfg.Compositions[0].NodePools[0].Label = ""
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for empty pool label")
	}
}

func TestValidate_ExplicitMode_EmptyPairs(t *testing.T) {
	cfg := explicitConfig()
	cfg.Compositions[0].NodePools[0].Pairs = nil
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for empty pairs in pool")
	}
}

func TestValidate_RailConfig(t *testing.T) {
	cfg := validConfig()
	cfg.RailConfig = &RailConfig{
		InterfacePrefix: "net",
		StartingTableID: 100,
		Rails: []RailEntry{
			{
				Selector: RailSelector{CEL: `device.attributes["dra.net"].ipv4.startsWith("10.0.0.")`},
				Config: RailParameters{
					Subnet:  "10.0.0.0/16",
					Gateway: "10.0.0.1",
					MTU:     9000,
					TableID: 100,
				},
			},
		},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid rail config, got: %v", err)
	}
}

func TestValidate_RailConfigMissingSubnet(t *testing.T) {
	cfg := validConfig()
	cfg.RailConfig = &RailConfig{
		InterfacePrefix: "net",
		Rails: []RailEntry{
			{
				Selector: RailSelector{CEL: "some expr"},
				Config:   RailParameters{Gateway: "10.0.0.1", MTU: 9000},
			},
		},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for missing subnet")
	}
}

func TestParse(t *testing.T) {
	yaml := `
driver:
  name: composite.dra.example.io
sources:
  - name: gpu
    driver: gpu.nvidia.com
    deviceClassName: gpu.nvidia.com
  - name: nic
    driver: dra.net
    deviceClassName: dranet
compositions:
  - name: gpu-nic-pair
    members:
      - source: gpu
        count: 1
      - source: nic
        count: 1
    constraints:
      - type: matchAttribute
        attribute: resource.kubernetes.io/pcieRoot
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cfg.Driver.Name != "composite.dra.example.io" {
		t.Fatalf("unexpected driver name: %s", cfg.Driver.Name)
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(cfg.Sources))
	}
	if len(cfg.Compositions) != 1 {
		t.Fatalf("expected 1 composition, got %d", len(cfg.Compositions))
	}
}
