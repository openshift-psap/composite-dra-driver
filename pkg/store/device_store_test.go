package store

import (
	"testing"
)

func TestDeviceStore_PutGet(t *testing.T) {
	s := NewDeviceStore()
	m := &DeviceMapping{
		CompositionName: "gpu-nic-pair",
		Members: []DeviceMember{
			{SourceName: "gpu", Driver: "gpu.nvidia.com", Pool: "pool-a", Device: "gpu-0"},
			{SourceName: "nic", Driver: "dra.net", Pool: "pool-b", Device: "nic-0"},
		},
	}

	s.Put("mypool", "dev-0", m)

	got := s.Get("mypool", "dev-0")
	if got == nil {
		t.Fatal("expected mapping, got nil")
	}
	if got.CompositionName != "gpu-nic-pair" {
		t.Errorf("composition = %s, want gpu-nic-pair", got.CompositionName)
	}
	if len(got.Members) != 2 {
		t.Errorf("members = %d, want 2", len(got.Members))
	}
}

func TestDeviceStore_GetMissing(t *testing.T) {
	s := NewDeviceStore()
	if got := s.Get("nonexistent", "dev"); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestDeviceStore_Delete(t *testing.T) {
	s := NewDeviceStore()
	s.Put("p", "d", &DeviceMapping{CompositionName: "test"})
	s.Delete("p", "d")
	if got := s.Get("p", "d"); got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestDeviceStore_ReplaceAll(t *testing.T) {
	s := NewDeviceStore()
	s.Put("p", "old", &DeviceMapping{CompositionName: "old"})

	s.ReplaceAll(map[string]*DeviceMapping{
		"p/new": {CompositionName: "new"},
	})

	if got := s.Get("p", "old"); got != nil {
		t.Fatal("old mapping should be gone after ReplaceAll")
	}
	if s.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", s.Len())
	}
}

func TestDeviceStore_Len(t *testing.T) {
	s := NewDeviceStore()
	if s.Len() != 0 {
		t.Fatalf("expected 0, got %d", s.Len())
	}
	s.Put("p", "a", &DeviceMapping{})
	s.Put("p", "b", &DeviceMapping{})
	if s.Len() != 2 {
		t.Fatalf("expected 2, got %d", s.Len())
	}
}

func TestDeviceStore_List(t *testing.T) {
	s := NewDeviceStore()
	s.Put("p", "a", &DeviceMapping{CompositionName: "x"})
	s.Put("p", "b", &DeviceMapping{CompositionName: "y"})

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
}
