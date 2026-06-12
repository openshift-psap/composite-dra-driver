// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"fmt"
	"sync"

	resourceapi "k8s.io/api/resource/v1"
)

// DeviceMember represents one underlying device in a composite group.
type DeviceMember struct {
	SourceName     string
	Driver         string
	Pool           string
	Device         string
	DeviceClassName string
	Attributes     map[string]resourceapi.DeviceAttribute
}

// DeviceMapping maps a composite device to its underlying members.
type DeviceMapping struct {
	CompositionName string
	Members         []DeviceMember
	RailIndex       int
	NUMANode        int
}

// DeviceStore is a thread-safe map of composite device IDs to their underlying device mappings.
type DeviceStore struct {
	mu      sync.RWMutex
	devices map[string]*DeviceMapping
}

func NewDeviceStore() *DeviceStore {
	return &DeviceStore{
		devices: make(map[string]*DeviceMapping),
	}
}

func key(pool, device string) string {
	return fmt.Sprintf("%s/%s", pool, device)
}

func (s *DeviceStore) Put(pool, device string, mapping *DeviceMapping) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices[key(pool, device)] = mapping
}

func (s *DeviceStore) Get(pool, device string) *DeviceMapping {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.devices[key(pool, device)]
}

func (s *DeviceStore) Delete(pool, device string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.devices, key(pool, device))
}

// ReplaceAll atomically replaces all mappings (used on resync).
func (s *DeviceStore) ReplaceAll(mappings map[string]*DeviceMapping) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices = mappings
}

func (s *DeviceStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.devices)
}

func (s *DeviceStore) List() map[string]*DeviceMapping {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]*DeviceMapping, len(s.devices))
	for k, v := range s.devices {
		result[k] = v
	}
	return result
}
