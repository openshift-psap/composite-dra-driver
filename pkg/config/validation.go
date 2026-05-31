package config

import (
	"fmt"
	"strings"
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
		if sourceNames[src.Name] {
			return fmt.Errorf("duplicate source name: %s", src.Name)
		}
		sourceNames[src.Name] = true
	}

	for i, comp := range cfg.Compositions {
		if comp.Name == "" {
			return fmt.Errorf("compositions[%d].name is required", i)
		}
		if len(comp.Members) < 2 {
			return fmt.Errorf("compositions[%d] needs at least 2 members", i)
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
		tm := comp.TransportMode
		if tm != "" && tm != "auto" && tm != "ethernet" && tm != "infiniband" {
			return fmt.Errorf("compositions[%d].transportMode must be auto, ethernet, or infiniband", i)
		}
	}

	if cfg.RailConfig != nil {
		if err := validateRailConfig(cfg.RailConfig); err != nil {
			return fmt.Errorf("railConfig: %w", err)
		}
	}

	return nil
}

func validateRailConfig(rc *RailConfig) error {
	if rc.InterfacePrefix == "" {
		return fmt.Errorf("interfacePrefix is required")
	}
	for i, rail := range rc.Rails {
		if rail.Selector.CEL == "" {
			return fmt.Errorf("rails[%d].selector.cel is required", i)
		}
		if rail.Config.Subnet == "" {
			return fmt.Errorf("rails[%d].config.subnet is required", i)
		}
		if rail.Config.Gateway == "" {
			return fmt.Errorf("rails[%d].config.gateway is required", i)
		}
		if rail.Config.MTU <= 0 {
			return fmt.Errorf("rails[%d].config.mtu must be > 0", i)
		}
		if !strings.Contains(rail.Config.Subnet, "/") {
			return fmt.Errorf("rails[%d].config.subnet must be in CIDR notation", i)
		}
	}
	return nil
}
