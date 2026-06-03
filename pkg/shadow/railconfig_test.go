package shadow

import (
	"encoding/json"
	"testing"

	resourceapi "k8s.io/api/resource/v1"

	"github.com/openshift-psap/composite-dra-driver/pkg/config"
)

func strAttr(s string) resourceapi.DeviceAttribute {
	return resourceapi.DeviceAttribute{StringValue: &s}
}

func testRailConfig() *config.RailConfig {
	return &config.RailConfig{
		InterfacePrefix: "net",
		StartingTableID: 100,
		Rails: []config.RailEntry{
			{
				Selector: config.RailSelector{CEL: `device.attributes["dra.net"].ipv4.startsWith("10.0.0.")`},
				Config: config.RailParameters{
					Subnet:  "10.0.0.0/16",
					Gateway: "10.0.0.1",
					MTU:     9000,
					TableID: 100,
				},
			},
			{
				Selector: config.RailSelector{CEL: `device.attributes["dra.net"].ipv4.startsWith("10.0.1.")`},
				Config: config.RailParameters{
					Subnet:  "10.0.1.0/16",
					Gateway: "10.0.1.1",
					MTU:     9000,
					TableID: 101,
				},
			},
		},
	}
}

func TestRailConfigResolver_MatchesRail0(t *testing.T) {
	r := NewRailConfigResolver(testRailConfig())
	attrs := map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4": strAttr("10.0.0.42"),
	}

	data, railIdx := r.ResolveForDevice(attrs, 0)
	if data == nil {
		t.Fatal("expected config, got nil")
	}
	if railIdx != 0 {
		t.Fatalf("expected rail 0, got %d", railIdx)
	}

	var params NICParams
	if err := json.Unmarshal(data, &params); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if params.Interface.Name != "net0" {
		t.Errorf("interface name = %s, want net0", params.Interface.Name)
	}
	if params.Interface.MTU != 9000 {
		t.Errorf("mtu = %d, want 9000", params.Interface.MTU)
	}
	if len(params.Routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(params.Routes))
	}
	if params.Routes[0].Table != 100 {
		t.Errorf("route table = %d, want 100", params.Routes[0].Table)
	}
	if params.Routes[1].Gateway != "10.0.0.1" {
		t.Errorf("gateway = %s, want 10.0.0.1", params.Routes[1].Gateway)
	}
	if len(params.Rules) != 1 || params.Rules[0].Table != 100 {
		t.Errorf("unexpected rules: %+v", params.Rules)
	}
}

func TestRailConfigResolver_MatchesRail1(t *testing.T) {
	r := NewRailConfigResolver(testRailConfig())
	attrs := map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4": strAttr("10.0.1.7"),
	}

	data, railIdx := r.ResolveForDevice(attrs, 3)
	if data == nil {
		t.Fatal("expected config, got nil")
	}
	if railIdx != 1 {
		t.Fatalf("expected rail 1, got %d", railIdx)
	}

	var params NICParams
	json.Unmarshal(data, &params)
	if params.Interface.Name != "net3" {
		t.Errorf("interface name = %s, want net3 (ordinal 3)", params.Interface.Name)
	}
	if params.Routes[1].Gateway != "10.0.1.1" {
		t.Errorf("gateway = %s, want 10.0.1.1", params.Routes[1].Gateway)
	}
}

func TestRailConfigResolver_NoMatch(t *testing.T) {
	r := NewRailConfigResolver(testRailConfig())
	attrs := map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4": strAttr("192.168.1.5"),
	}

	data, railIdx := r.ResolveForDevice(attrs, 0)
	if data != nil {
		t.Fatalf("expected nil for non-matching IP, got %d bytes", len(data))
	}
	if railIdx != -1 {
		t.Fatalf("expected rail -1, got %d", railIdx)
	}
}

func TestRailConfigResolver_NilConfig(t *testing.T) {
	r := NewRailConfigResolver(nil)
	data, railIdx := r.ResolveForDevice(map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4": strAttr("10.0.0.1"),
	}, 0)
	if data != nil {
		t.Fatal("expected nil for nil config")
	}
	if railIdx != -1 {
		t.Fatalf("expected -1, got %d", railIdx)
	}
}

func TestExtractStartsWithPrefix(t *testing.T) {
	tests := []struct {
		cel      string
		expected string
	}{
		{`device.attributes["dra.net"].ipv4.startsWith("10.0.0.")`, "10.0.0."},
		{`device.attributes["dra.net/ipv4"].startsWith("10.0.1.")`, "10.0.1."},
		{`some.other.expression`, ""},
		{`startsWith("abc")`, "abc"},
	}

	for _, tt := range tests {
		got := extractStartsWithPrefix(tt.cel)
		if got != tt.expected {
			t.Errorf("extractStartsWithPrefix(%q) = %q, want %q", tt.cel, got, tt.expected)
		}
	}
}
