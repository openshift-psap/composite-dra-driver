package shadow

import (
	"encoding/json"
	"fmt"
	"strings"

	resourceapi "k8s.io/api/resource/v1"

	"github.com/openshift-psap/composite-dra-driver/pkg/config"
)

// NICParams is the opaque driver config structure dranet expects.
type NICParams struct {
	Interface InterfaceConfig `json:"interface"`
	Routes    []Route         `json:"routes,omitempty"`
	Rules     []Rule          `json:"rules,omitempty"`
}

type InterfaceConfig struct {
	Name string `json:"name"`
	MTU  int    `json:"mtu,omitempty"`
}

type Route struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway,omitempty"`
	Scope       int    `json:"scope,omitempty"`
	Table       int    `json:"table,omitempty"`
}

type Rule struct {
	Source   string `json:"source"`
	Table    int    `json:"table"`
	Priority int    `json:"priority"`
}

// RailConfigResolver matches NIC device attributes against rail selectors
// and generates opaque driver config for shadow claims.
type RailConfigResolver struct {
	cfg *config.RailConfig
}

func NewRailConfigResolver(cfg *config.RailConfig) *RailConfigResolver {
	if cfg == nil {
		return &RailConfigResolver{}
	}
	return &RailConfigResolver{cfg: cfg}
}

// ResolveForDevice returns the opaque config JSON for a NIC device based on its attributes.
// pairOrdinal is the pair's index within the pod (0, 1, 2, ...) — used for interface naming
// to avoid collisions when multiple pairs land on the same rail.
// Returns nil if no rail config is defined or no rail matches.
func (r *RailConfigResolver) ResolveForDevice(attrs map[string]resourceapi.DeviceAttribute, pairOrdinal int) ([]byte, int) {
	if r.cfg == nil || len(r.cfg.Rails) == 0 {
		return nil, -1
	}

	for i, rail := range r.cfg.Rails {
		if matchesRailSelector(attrs, rail.Selector) {
			params := NICParams{
				Interface: InterfaceConfig{
					Name: fmt.Sprintf("%s%d", r.cfg.InterfacePrefix, pairOrdinal),
					MTU:  rail.Config.MTU,
				},
				Routes: []Route{
					{
						Destination: rail.Config.Subnet,
						Scope:       253,
						Table:       rail.Config.TableID,
					},
					{
						Destination: "0.0.0.0/0",
						Gateway:     rail.Config.Gateway,
						Table:       rail.Config.TableID,
					},
				},
				Rules: []Rule{
					{
						Source:   rail.Config.Subnet,
						Table:    rail.Config.TableID,
						Priority: 32765,
					},
				},
			}

			data, _ := json.Marshal(params)
			return data, i
		}
	}

	return nil, -1
}

// matchesRailSelector evaluates a rail's CEL selector against device attributes.
// Currently supports simple ipv4.startsWith() patterns extracted from the CEL expression.
func matchesRailSelector(attrs map[string]resourceapi.DeviceAttribute, selector config.RailSelector) bool {
	if selector.CEL == "" {
		return false
	}

	// Extract startsWith pattern from CEL like:
	// device.attributes["dra.net"].ipv4.startsWith("10.0.0.")
	// or device.attributes["dra.net/ipv4"].startsWith("10.0.0.")
	prefix := extractStartsWithPrefix(selector.CEL)
	if prefix == "" {
		return false
	}

	for key, attr := range attrs {
		if !strings.Contains(key, "ipv4") {
			continue
		}
		if attr.StringValue != nil && strings.HasPrefix(*attr.StringValue, prefix) {
			return true
		}
	}
	return false
}

func extractStartsWithPrefix(cel string) string {
	idx := strings.Index(cel, "startsWith(\"")
	if idx < 0 {
		return ""
	}
	start := idx + len("startsWith(\"")
	end := strings.Index(cel[start:], "\")")
	if end < 0 {
		return ""
	}
	return cel[start : start+end]
}
