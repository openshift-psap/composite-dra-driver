// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthesizer

import (
	"testing"

	resourceapi "k8s.io/api/resource/v1"
)

func TestCELFilter_BoolMatch(t *testing.T) {
	f, err := NewCELFilter()
	if err != nil {
		t.Fatal(err)
	}

	rdmaTrue := true
	attrs := map[string]resourceapi.DeviceAttribute{
		"dra.net/rdma": {BoolValue: &rdmaTrue},
	}

	if !f.Match(`device.attributes["dra.net"].rdma == true`, attrs) {
		t.Error("expected rdma==true to match")
	}
	if f.Match(`device.attributes["dra.net"].rdma == false`, attrs) {
		t.Error("expected rdma==false to not match")
	}
}

func TestCELFilter_StringStartsWith(t *testing.T) {
	f, err := NewCELFilter()
	if err != nil {
		t.Fatal(err)
	}

	ip := "10.0.0.42"
	attrs := map[string]resourceapi.DeviceAttribute{
		"dra.net/ipv4": {StringValue: &ip},
	}

	if !f.Match(`device.attributes["dra.net"].ipv4.startsWith("10.0.0.")`, attrs) {
		t.Error("expected 10.0.0.42 to match startsWith 10.0.0.")
	}
	if f.Match(`device.attributes["dra.net"].ipv4.startsWith("10.0.1.")`, attrs) {
		t.Error("expected 10.0.0.42 to not match startsWith 10.0.1.")
	}
}

func TestCELFilter_IntComparison(t *testing.T) {
	f, err := NewCELFilter()
	if err != nil {
		t.Fatal(err)
	}

	numa := int64(0)
	attrs := map[string]resourceapi.DeviceAttribute{
		"dra.net/numaNode": {IntValue: &numa},
	}

	if !f.Match(`device.attributes["dra.net"].numaNode == 0`, attrs) {
		t.Error("expected numaNode==0 to match")
	}
	if f.Match(`device.attributes["dra.net"].numaNode == 1`, attrs) {
		t.Error("expected numaNode==1 to not match")
	}
}

func TestCELFilter_MissingAttribute(t *testing.T) {
	f, err := NewCELFilter()
	if err != nil {
		t.Fatal(err)
	}

	attrs := map[string]resourceapi.DeviceAttribute{}

	// Missing attribute should not match (CEL eval returns error → false)
	if f.Match(`device.attributes["dra.net"].rdma == true`, attrs) {
		t.Error("expected missing attribute to not match")
	}
}

func TestCELFilter_CombinedExpression(t *testing.T) {
	f, err := NewCELFilter()
	if err != nil {
		t.Fatal(err)
	}

	rdma := true
	ip := "10.0.0.5"
	attrs := map[string]resourceapi.DeviceAttribute{
		"dra.net/rdma": {BoolValue: &rdma},
		"dra.net/ipv4": {StringValue: &ip},
	}

	expr := `device.attributes["dra.net"].rdma == true && device.attributes["dra.net"].ipv4.startsWith("10.0.")`
	if !f.Match(expr, attrs) {
		t.Error("expected combined expression to match")
	}
}

func TestCELFilter_InvalidExpression(t *testing.T) {
	f, err := NewCELFilter()
	if err != nil {
		t.Fatal(err)
	}

	attrs := map[string]resourceapi.DeviceAttribute{}

	// Invalid CEL should return false, not panic
	if f.Match(`this is not valid CEL!!!`, attrs) {
		t.Error("expected invalid CEL to return false")
	}
}

func TestCELFilter_Caching(t *testing.T) {
	f, err := NewCELFilter()
	if err != nil {
		t.Fatal(err)
	}

	rdma := true
	attrs := map[string]resourceapi.DeviceAttribute{
		"dra.net/rdma": {BoolValue: &rdma},
	}

	expr := `device.attributes["dra.net"].rdma == true`

	// Call twice — second should use cached program
	f.Match(expr, attrs)
	if !f.Match(expr, attrs) {
		t.Error("cached evaluation failed")
	}

	if len(f.cache) != 1 {
		t.Errorf("expected 1 cached program, got %d", len(f.cache))
	}
}
