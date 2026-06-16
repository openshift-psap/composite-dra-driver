// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package shadow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	resourceapi "k8s.io/api/resource/v1"
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

func writeParamsFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "params.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolve_MatchByPrefix(t *testing.T) {
	path := writeParamsFile(t, `
nic:
  params: '{"gw":"{{.Gateway}}","ord":{{.PairOrdinal}}}'
  entries:
    - match:
        "dra.net/ipv4":
          prefix: "10.0."
      values:
        Gateway: "10.0.0.1"
    - match:
        "dra.net/ipv4":
          prefix: "10.1."
      values:
        Gateway: "10.1.0.1"
`)
	r, err := NewDeviceParamsResolver(path, "node-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	attrs := map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4": strAttr("10.0.0.5/16"),
	}
	result := r.ResolveForDevice("nic", attrs, 3)
	if result == nil {
		t.Fatal("expected result, got nil")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("invalid JSON: %s", string(result))
	}
	if parsed["gw"] != "10.0.0.1" {
		t.Errorf("gateway = %v, want 10.0.0.1", parsed["gw"])
	}
	if parsed["ord"] != float64(3) {
		t.Errorf("ordinal = %v, want 3", parsed["ord"])
	}
}

func TestResolve_MatchByExact(t *testing.T) {
	path := writeParamsFile(t, `
nic:
  params: '{"mode":"{{.Mode}}"}'
  entries:
    - match:
        "dra.net/rdma":
          exact: "true"
      values:
        Mode: "rdma"
    - match:
        "dra.net/rdma":
          exact: "false"
      values:
        Mode: "standard"
`)
	r, err := NewDeviceParamsResolver(path, "node-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	result := r.ResolveForDevice("nic", map[string]resourceapi.DeviceAttribute{
		"dra.net/rdma": boolAttr(true),
	}, 0)
	if result == nil {
		t.Fatal("expected result")
	}

	var parsed map[string]interface{}
	json.Unmarshal(result, &parsed)
	if parsed["mode"] != "rdma" {
		t.Errorf("mode = %v, want rdma", parsed["mode"])
	}
}

func TestResolve_MultiAttributeAND(t *testing.T) {
	path := writeParamsFile(t, `
nic:
  params: '{"table":{{.Table}}}'
  entries:
    - match:
        "dra.net/ipv4":
          prefix: "10.0."
        "dra.net/numaNode":
          exact: "0"
      values:
        Table: 100
    - match:
        "dra.net/ipv4":
          prefix: "10.0."
        "dra.net/numaNode":
          exact: "1"
      values:
        Table: 200
`)
	r, err := NewDeviceParamsResolver(path, "node-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	attrs := map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4":     strAttr("10.0.0.5/16"),
		"dra.net/numaNode": intAttr(1),
	}
	result := r.ResolveForDevice("nic", attrs, 0)
	if result == nil {
		t.Fatal("expected result")
	}

	var parsed map[string]interface{}
	json.Unmarshal(result, &parsed)
	if parsed["table"] != float64(200) {
		t.Errorf("table = %v, want 200", parsed["table"])
	}
}

func TestResolve_CatchAll(t *testing.T) {
	path := writeParamsFile(t, `
gpu:
  params: '{"mode":"{{.ComputeMode}}"}'
  entries:
    - values:
        ComputeMode: "exclusive"
`)
	r, err := NewDeviceParamsResolver(path, "node-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	result := r.ResolveForDevice("gpu", map[string]resourceapi.DeviceAttribute{
		"gpu.nvidia.com/model": strAttr("B200"),
	}, 0)
	if result == nil {
		t.Fatal("expected result")
	}
	if string(result) != `{"mode":"exclusive"}` {
		t.Errorf("got %s", string(result))
	}
}

func TestResolve_NoMatch(t *testing.T) {
	path := writeParamsFile(t, `
nic:
  params: '{}'
  entries:
    - match:
        "dra.net/ipv4":
          prefix: "10.0."
      values: {}
`)
	r, err := NewDeviceParamsResolver(path, "node-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	result := r.ResolveForDevice("nic", map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4": strAttr("172.16.1.1/24"),
	}, 0)
	if result != nil {
		t.Errorf("expected nil, got %s", string(result))
	}
}

func TestResolve_UnknownSource(t *testing.T) {
	path := writeParamsFile(t, `
nic:
  params: '{}'
  entries:
    - values: {}
`)
	r, err := NewDeviceParamsResolver(path, "node-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	result := r.ResolveForDevice("storage", map[string]resourceapi.DeviceAttribute{}, 0)
	if result != nil {
		t.Errorf("expected nil for unknown source, got %s", string(result))
	}
}

func TestResolve_Overrides_NodeSelector(t *testing.T) {
	path := writeParamsFile(t, `
nic:
  params: '{"gw":"{{.Gateway}}"}'
  entries:
    - match:
        "dra.net/ipv4":
          prefix: "10.0."
      values:
        Gateway: "10.0.0.1"
  overrides:
    - nodeSelector:
        kubernetes.io/hostname: "gpu-node-1"
      entries:
        - match:
            "dra.net/ipv4":
              prefix: "10.0."
          values:
            Gateway: "10.0.1.254"
`)
	nodeLabels := map[string]string{
		"kubernetes.io/hostname": "gpu-node-1",
	}
	r, err := NewDeviceParamsResolver(path, "gpu-node-1", nodeLabels)
	if err != nil {
		t.Fatal(err)
	}

	result := r.ResolveForDevice("nic", map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4": strAttr("10.0.0.5/16"),
	}, 0)
	if result == nil {
		t.Fatal("expected result")
	}

	var parsed map[string]interface{}
	json.Unmarshal(result, &parsed)
	if parsed["gw"] != "10.0.1.254" {
		t.Errorf("gateway = %v, want 10.0.1.254 (override)", parsed["gw"])
	}
}

func TestResolve_Overrides_NoMatch(t *testing.T) {
	path := writeParamsFile(t, `
nic:
  params: '{"gw":"{{.Gateway}}"}'
  entries:
    - match:
        "dra.net/ipv4":
          prefix: "10.0."
      values:
        Gateway: "10.0.0.1"
  overrides:
    - nodeSelector:
        kubernetes.io/hostname: "gpu-node-2"
      entries:
        - match:
            "dra.net/ipv4":
              prefix: "10.0."
          values:
            Gateway: "10.0.2.254"
`)
	nodeLabels := map[string]string{
		"kubernetes.io/hostname": "gpu-node-1",
	}
	r, err := NewDeviceParamsResolver(path, "gpu-node-1", nodeLabels)
	if err != nil {
		t.Fatal(err)
	}

	result := r.ResolveForDevice("nic", map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4": strAttr("10.0.0.5/16"),
	}, 0)

	var parsed map[string]interface{}
	json.Unmarshal(result, &parsed)
	if parsed["gw"] != "10.0.0.1" {
		t.Errorf("gateway = %v, want 10.0.0.1 (default, override didn't match)", parsed["gw"])
	}
}

func TestResolve_Overrides_MultiLevel(t *testing.T) {
	path := writeParamsFile(t, `
nic:
  params: '{"gw":"{{.Gateway}}","table":{{.Table}}}'
  entries:
    - match:
        "dra.net/ipv4":
          prefix: "10.0."
      values:
        Gateway: "10.0.0.1"
        Table: 100
  overrides:
    - nodeSelector:
        topology.kubernetes.io/rack: "rack-a"
      entries:
        - match:
            "dra.net/ipv4":
              prefix: "10.0."
          values:
            Table: 200
    - nodeSelector:
        kubernetes.io/hostname: "rack-a-node-3"
      entries:
        - match:
            "dra.net/ipv4":
              prefix: "10.0."
          values:
            Gateway: "10.0.3.254"
`)
	nodeLabels := map[string]string{
		"kubernetes.io/hostname":       "rack-a-node-3",
		"topology.kubernetes.io/rack":  "rack-a",
	}
	r, err := NewDeviceParamsResolver(path, "rack-a-node-3", nodeLabels)
	if err != nil {
		t.Fatal(err)
	}

	result := r.ResolveForDevice("nic", map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4": strAttr("10.0.0.5/16"),
	}, 0)

	var parsed map[string]interface{}
	json.Unmarshal(result, &parsed)
	if parsed["gw"] != "10.0.3.254" {
		t.Errorf("gateway = %v, want 10.0.3.254 (node override)", parsed["gw"])
	}
	if parsed["table"] != float64(200) {
		t.Errorf("table = %v, want 200 (rack override)", parsed["table"])
	}
}

func TestResolve_DeviceFunction(t *testing.T) {
	path := writeParamsFile(t, `
nic:
  params: '{"mtu":{{device "dra.net/mtu"}}}'
  entries:
    - values: {}
`)
	r, err := NewDeviceParamsResolver(path, "node-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	result := r.ResolveForDevice("nic", map[string]resourceapi.DeviceAttribute{
		"dra.net/mtu": intAttr(9000),
	}, 0)
	if result == nil {
		t.Fatal("expected result")
	}
	if string(result) != `{"mtu":9000}` {
		t.Errorf("got %s, want {\"mtu\":9000}", string(result))
	}
}

func TestResolve_NetworkFunction(t *testing.T) {
	path := writeParamsFile(t, `
nic:
  params: '{"subnet":"{{network (device "dra.net/ipv4")}}"}'
  entries:
    - values: {}
`)
	r, err := NewDeviceParamsResolver(path, "node-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	result := r.ResolveForDevice("nic", map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4": strAttr("10.0.0.5/16"),
	}, 0)
	if result == nil {
		t.Fatal("expected result")
	}
	if string(result) != `{"subnet":"10.0.0.0/16"}` {
		t.Errorf("got %s", string(result))
	}
}

func TestResolve_CrossRailsRange(t *testing.T) {
	path := writeParamsFile(t, `
nic:
  params: |
    {"routes":[{{range $i, $r := .CrossRails}}{{if $i}},{{end}}{"dst":"{{$r}}","gw":"{{$.Gateway}}"}{{end}}]}
  entries:
    - match:
        "dra.net/ipv4":
          prefix: "10.0."
      values:
        Gateway: "10.0.0.1"
        CrossRails:
          - "10.1.0.0/16"
          - "10.2.0.0/16"
`)
	r, err := NewDeviceParamsResolver(path, "node-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	result := r.ResolveForDevice("nic", map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4": strAttr("10.0.0.5/16"),
	}, 0)
	if result == nil {
		t.Fatal("expected result")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("invalid JSON: %s", string(result))
	}
	routes := parsed["routes"].([]interface{})
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}
	r0 := routes[0].(map[string]interface{})
	if r0["dst"] != "10.1.0.0/16" || r0["gw"] != "10.0.0.1" {
		t.Errorf("route 0 = %v", r0)
	}
}

func TestNewDeviceParamsResolver_FileMissing(t *testing.T) {
	_, err := NewDeviceParamsResolver("/nonexistent/path.yaml", "node-1", nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestNetworkCIDR(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"10.0.0.5/16", "10.0.0.0/16"},
		{"172.16.1.1/24", "172.16.1.0/24"},
		{"192.168.1.100/32", "192.168.1.100/32"},
		{"invalid", "invalid"},
	}
	for _, tt := range tests {
		got := networkCIDR(tt.input)
		if got != tt.want {
			t.Errorf("networkCIDR(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
