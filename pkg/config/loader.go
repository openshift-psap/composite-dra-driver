// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

func LoadFromFile(path string) (*CompositeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}
	return Parse(data)
}

func Parse(data []byte) (*CompositeConfig, error) {
	cfg := &CompositeConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
