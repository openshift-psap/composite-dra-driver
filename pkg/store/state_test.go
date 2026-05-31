package store

import (
	"os"
	"path/filepath"
	"testing"
)

func tempDB(t *testing.T) *StateStore {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStateStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open state store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStateStore_SaveAndGet(t *testing.T) {
	s := tempDB(t)

	record := ShadowRecord{
		CompositeClaimUID: "uid-123",
		Namespace:         "default",
		Shadows: []ShadowEntry{
			{DriverName: "gpu.nvidia.com", Namespace: "default", Name: "shadow-gpu", UID: "s-uid-1"},
			{DriverName: "dra.net", Namespace: "default", Name: "shadow-nic", UID: "s-uid-2"},
		},
	}

	if err := s.SaveShadows(record); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.GetShadows("uid-123")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected record, got nil")
	}
	if len(got.Shadows) != 2 {
		t.Fatalf("expected 2 shadows, got %d", len(got.Shadows))
	}
	if got.Shadows[0].DriverName != "gpu.nvidia.com" {
		t.Errorf("shadow[0].driver = %s, want gpu.nvidia.com", got.Shadows[0].DriverName)
	}
}

func TestStateStore_GetMissing(t *testing.T) {
	s := tempDB(t)
	got, err := s.GetShadows("nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestStateStore_Delete(t *testing.T) {
	s := tempDB(t)
	s.SaveShadows(ShadowRecord{CompositeClaimUID: "uid-1", Shadows: []ShadowEntry{{Name: "x"}}})
	s.DeleteShadows("uid-1")

	got, _ := s.GetShadows("uid-1")
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestStateStore_ListAll(t *testing.T) {
	s := tempDB(t)
	s.SaveShadows(ShadowRecord{CompositeClaimUID: "a", Shadows: []ShadowEntry{{Name: "x"}}})
	s.SaveShadows(ShadowRecord{CompositeClaimUID: "b", Shadows: []ShadowEntry{{Name: "y"}}})

	all, err := s.ListAll()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}
}

func TestStateStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	s1, _ := NewStateStore(dbPath)
	s1.SaveShadows(ShadowRecord{CompositeClaimUID: "persist-uid", Shadows: []ShadowEntry{{Name: "z"}}})
	s1.Close()

	s2, _ := NewStateStore(dbPath)
	defer s2.Close()
	got, _ := s2.GetShadows("persist-uid")
	if got == nil {
		t.Fatal("expected record to survive reopen")
	}
}

func TestStateStore_BadPath(t *testing.T) {
	_, err := NewStateStore(filepath.Join(os.DevNull, "impossible", "path.db"))
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}
