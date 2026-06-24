// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
)

func Validate(cfg *CompositeConfig) error {
	if cfg.Driver.Name == "" {
		return fmt.Errorf("driver.name is required")
	}
	if len(cfg.Sources) == 0 {
		return fmt.Errorf("at least one source is required")
	}
	if len(cfg.Compositions) == 0 {
		return fmt.Errorf("at least one composition is required")
	}

	sourceNames := make(map[string]bool)
	for i, src := range cfg.Sources {
		if src.Name == "" {
			return fmt.Errorf("sources[%d].name is required", i)
		}
		if src.Driver == "" {
			return fmt.Errorf("sources[%d].driver is required", i)
		}
		if src.DeviceClassName == "" {
			return fmt.Errorf("sources[%d].deviceClassName is required", i)
		}
		if src.Driver == cfg.Driver.Name {
			return fmt.Errorf("sources[%d].driver must not reference the composite driver itself (%s); multi-level composition is not supported", i, src.Driver)
		}
		if sourceNames[src.Name] {
			return fmt.Errorf("duplicate source name: %s", src.Name)
		}
		sourceNames[src.Name] = true
	}

	compNames := make(map[string]bool)
	deviceClassNames := make(map[string]bool)

	for i, comp := range cfg.Compositions {
		if comp.Name == "" {
			return fmt.Errorf("compositions[%d].name is required", i)
		}
		if compNames[comp.Name] {
			return fmt.Errorf("duplicate composition name: %s", comp.Name)
		}
		compNames[comp.Name] = true

		dcn := comp.EffectiveDeviceClassName()
		if deviceClassNames[dcn] {
			return fmt.Errorf("duplicate effective deviceClassName: %s (from compositions[%d])", dcn, i)
		}
		deviceClassNames[dcn] = true

		if len(comp.Members) < 1 {
			return fmt.Errorf("compositions[%d] needs at least 1 member", i)
		}
		for j, member := range comp.Members {
			if !sourceNames[member.Source] {
				return fmt.Errorf("compositions[%d].members[%d] references unknown source %q", i, j, member.Source)
			}
			if member.Count < 1 {
				return fmt.Errorf("compositions[%d].members[%d].count must be >= 1", i, j)
			}
		}
		for j, c := range comp.Constraints {
			if c.Type == "" {
				return fmt.Errorf("compositions[%d].constraints[%d].type is required", i, j)
			}
			if c.Type != "matchAttribute" {
				return fmt.Errorf("compositions[%d].constraints[%d]: unsupported type %q (only matchAttribute supported)", i, j, c.Type)
			}
			if c.Attribute == "" {
				return fmt.Errorf("compositions[%d].constraints[%d].attribute is required", i, j)
			}
		}
		for name := range comp.Filters {
			if !sourceNames[name] {
				return fmt.Errorf("compositions[%d].filters references unknown source %q", i, name)
			}
		}

		pm := comp.PairingMode
		if pm != "" && pm != "auto" && pm != "explicit" {
			return fmt.Errorf("compositions[%d].pairingMode must be auto or explicit", i)
		}

		if pm != "explicit" && len(comp.Constraints) == 0 {
			uniqueSources := make(map[string]bool)
			for _, m := range comp.Members {
				uniqueSources[m.Source] = true
			}
			if len(uniqueSources) > 1 {
				return fmt.Errorf("compositions[%d]: auto pairing with multiple sources requires at least one constraint", i)
			}
		}

		if pm == "explicit" {
			if len(comp.NodePools) == 0 {
				return fmt.Errorf("compositions[%d]: pairingMode explicit requires at least one nodePools entry", i)
			}
			if comp.NodePoolLabelKey == "" {
				return fmt.Errorf("compositions[%d]: pairingMode explicit requires nodePoolLabelKey", i)
			}
			if len(comp.Constraints) > 0 {
				return fmt.Errorf("compositions[%d]: pairingMode explicit cannot be used with constraints", i)
			}
			memberSources := make(map[string]bool, len(comp.Members))
			for _, m := range comp.Members {
				memberSources[m.Source] = true
			}
			poolLabels := make(map[string]bool)
			for j, np := range comp.NodePools {
				if np.Label == "" {
					return fmt.Errorf("compositions[%d].nodePools[%d].label is required", i, j)
				}
				if poolLabels[np.Label] {
					return fmt.Errorf("compositions[%d].nodePools[%d]: duplicate label %q", i, j, np.Label)
				}
				poolLabels[np.Label] = true
				if len(np.Pairs) == 0 {
					return fmt.Errorf("compositions[%d].nodePools[%d]: at least one pair is required", i, j)
				}
				for k, ep := range np.Pairs {
					for src, celExpr := range ep.Selectors {
						if !sourceNames[src] {
							return fmt.Errorf("compositions[%d].nodePools[%d].pairs[%d].selectors references unknown source %q", i, j, k, src)
						}
						if !memberSources[src] {
							return fmt.Errorf("compositions[%d].nodePools[%d].pairs[%d].selectors references source %q not in members", i, j, k, src)
						}
						if celExpr == "" {
							return fmt.Errorf("compositions[%d].nodePools[%d].pairs[%d].selectors[%s]: CEL expression is required", i, j, k, src)
						}
					}
					for src := range memberSources {
						if _, ok := ep.Selectors[src]; !ok {
							return fmt.Errorf("compositions[%d].nodePools[%d].pairs[%d].selectors missing required source %q", i, j, k, src)
						}
					}
					if ep.Rail < 0 {
						return fmt.Errorf("compositions[%d].nodePools[%d].pairs[%d].rail must be >= 0", i, j, k)
					}
					if ep.NUMA < 0 {
						return fmt.Errorf("compositions[%d].nodePools[%d].pairs[%d].numa must be >= 0", i, j, k)
					}
				}
			}
		} else if len(comp.NodePools) > 0 {
			return fmt.Errorf("compositions[%d]: nodePools requires pairingMode explicit", i)
		}
		tm := comp.TransportMode
		if tm != "" && tm != "auto" && tm != "ethernet" && tm != "infiniband" {
			return fmt.Errorf("compositions[%d].transportMode must be auto, ethernet, or infiniband", i)
		}
	}

	if cfg.DeviceParams != nil {
		if cfg.DeviceParams.ConfigMapPath == "" {
			return fmt.Errorf("deviceParams.configMapPath is required when deviceParams is set")
		}
	}

	return nil
}
