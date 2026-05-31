package synthesizer

import (
	"testing"

	resourceapi "k8s.io/api/resource/v1"

	"github.com/openshift-psap/composite-dra-driver/pkg/store"
)

func strAttrPub(s string) resourceapi.DeviceAttribute {
	return resourceapi.DeviceAttribute{StringValue: &s}
}

func TestBuildResourceSlices_Empty(t *testing.T) {
	slices := BuildResourceSlices("test-driver", "node-1", nil)
	if len(slices) != 0 {
		t.Fatalf("expected 0 slices for empty input, got %d", len(slices))
	}
}

func TestBuildResourceSlices_SingleDevice(t *testing.T) {
	devices := []CompositeDevice{
		{
			Name: "gpu-0--nic-0",
			Attributes: map[string]resourceapi.DeviceAttribute{
				"resource.kubernetes.io/pcieRoot": strAttrPub("root-0"),
			},
			Mapping: &store.DeviceMapping{CompositionName: "gpu-nic-pair"},
		},
	}

	slices := BuildResourceSlices("composite.dra.example.io", "node-1", devices)
	if len(slices) != 1 {
		t.Fatalf("expected 1 slice, got %d", len(slices))
	}

	spec := slices[0]
	if spec.Driver != "composite.dra.example.io" {
		t.Errorf("driver = %s, want composite.dra.example.io", spec.Driver)
	}
	if *spec.NodeName != "node-1" {
		t.Errorf("nodeName = %s, want node-1", *spec.NodeName)
	}
	if len(spec.Devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(spec.Devices))
	}
	if spec.Devices[0].Name != "gpu-0--nic-0" {
		t.Errorf("device name = %s, want gpu-0--nic-0", spec.Devices[0].Name)
	}

	pcieRoot, ok := spec.Devices[0].Attributes[resourceapi.QualifiedName("resource.kubernetes.io/pcieRoot")]
	if !ok {
		t.Fatal("missing pcieRoot attribute")
	}
	if *pcieRoot.StringValue != "root-0" {
		t.Errorf("pcieRoot = %s, want root-0", *pcieRoot.StringValue)
	}
}

func TestBuildResourceSlices_SplitsAtLimit(t *testing.T) {
	devices := make([]CompositeDevice, 200)
	for i := range devices {
		devices[i] = CompositeDevice{
			Name:       "dev-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Attributes: map[string]resourceapi.DeviceAttribute{},
			Mapping:    &store.DeviceMapping{},
		}
	}

	slices := BuildResourceSlices("test", "node-1", devices)
	if len(slices) != 2 {
		t.Fatalf("expected 2 slices for 200 devices, got %d", len(slices))
	}
	if len(slices[0].Devices) != 128 {
		t.Errorf("first slice should have 128 devices, got %d", len(slices[0].Devices))
	}
	if len(slices[1].Devices) != 72 {
		t.Errorf("second slice should have 72 devices, got %d", len(slices[1].Devices))
	}
	for _, s := range slices {
		if s.Pool.ResourceSliceCount != 2 {
			t.Errorf("ResourceSliceCount = %d, want 2", s.Pool.ResourceSliceCount)
		}
	}
}

func TestBuildResourceSlices_PoolName(t *testing.T) {
	devices := []CompositeDevice{
		{Name: "d", Attributes: map[string]resourceapi.DeviceAttribute{}, Mapping: &store.DeviceMapping{}},
	}
	slices := BuildResourceSlices("composite.dra.example.io", "worker-3", devices)
	expected := "composite.dra.example.io-worker-3"
	if slices[0].Pool.Name != expected {
		t.Errorf("pool name = %s, want %s", slices[0].Pool.Name, expected)
	}
}

func TestLogPublisher(t *testing.T) {
	p := &LogPublisher{}
	err := p.Publish("test-driver", "node-1", []CompositeDevice{
		{Name: "d", Attributes: map[string]resourceapi.DeviceAttribute{}, Mapping: &store.DeviceMapping{}},
	})
	if err != nil {
		t.Fatalf("log publisher should not error: %v", err)
	}
}
