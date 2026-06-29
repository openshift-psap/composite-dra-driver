// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package config

// CompositeConfig is the top-level configuration for the composite DRA driver.
type CompositeConfig struct {
	Driver           DriverConfig          `json:"driver"`
	Sources          []SourceConfig        `json:"sources"`
	Compositions     []CompositionConfig   `json:"compositions"`
	DeviceParams     *DeviceParamsConfig   `json:"deviceParams,omitempty"`
	ExplicitPairings *ExplicitPairings     `json:"explicitPairings,omitempty"`
}

// DeviceParamsConfig references an externally-maintained ConfigMap
// that provides opaque driver params for shadow claims.
type DeviceParamsConfig struct {
	ConfigMapPath string `json:"configMapPath"`
}

type DriverConfig struct {
	Name string `json:"name"`
}

// SourceConfig defines an underlying DRA driver whose devices participate in compositions.
// Driver must reference a leaf (non-composite) DRA driver; multi-level composition is not supported.
type SourceConfig struct {
	Name             string              `json:"name"`
	Driver           string              `json:"driver"`
	DeviceClassName  string              `json:"deviceClassName"`
	ForwardAttributes []AttributeGroup   `json:"forwardAttributes"`
	SocketPath       string              `json:"socketPath,omitempty"`
}

type AttributeGroup struct {
	Domain     string   `json:"domain"`
	Attributes []string `json:"attributes"`
}

// CompositionConfig defines how devices from multiple sources are combined.
type CompositionConfig struct {
	Name                 string                       `json:"name"`
	Members              []MemberConfig               `json:"members"`
	Constraints          []ConstraintConfig           `json:"constraints"`
	Filters              map[string]FilterConfig      `json:"filters,omitempty"`
	PairingMode          string                       `json:"pairingMode,omitempty"`
	TransportMode        string                       `json:"transportMode,omitempty"`
	DeviceClassName      string                       `json:"deviceClassName,omitempty"`
	ExtendedResourceName string                       `json:"extendedResourceName,omitempty"`
	NodePoolLabelKey     string                       `json:"nodePoolLabelKey,omitempty"`
	NodePools            []ExplicitNodePool           `json:"nodePools,omitempty"`
	Priority             int                          `json:"priority,omitempty"`
}

func (c *CompositionConfig) EffectiveDeviceClassName() string {
	if c.DeviceClassName != "" {
		return c.DeviceClassName
	}
	return "composite-" + c.Name
}

func (c *CompositionConfig) EffectiveExtendedResourceName(driverName string) string {
	if c.ExtendedResourceName != "" {
		return c.ExtendedResourceName
	}
	return driverName + "/" + c.Name
}

// ExplicitNodePool groups explicit device pairs for nodes sharing a MachineConfigPool label value.
type ExplicitNodePool struct {
	Label string               `json:"label"`
	Pairs []ExplicitPairConfig `json:"pairs"`
}

// ExplicitPairConfig defines one admin-specified device grouping using CEL selectors.
type ExplicitPairConfig struct {
	Selectors map[string]string `json:"selectors"`
	Rail      int               `json:"rail"`
	NUMA      int               `json:"numa"`
}

type MemberConfig struct {
	Source string `json:"source"`
	Count  int    `json:"count"`
}

type ConstraintConfig struct {
	Type      string `json:"type"`
	Attribute string `json:"attribute"`
}

type FilterConfig struct {
	CEL string `json:"cel"`
}

// Deprecated: ExplicitPairings is the old top-level explicit pairing config.
// Use CompositionConfig.ExplicitPairs with CEL selectors instead.
type ExplicitPairings struct {
	NodePoolLabelKey string             `json:"nodePoolLabelKey"`
	NodePools        []NodePoolConfig   `json:"nodePools"`
}

type NodePoolConfig struct {
	Label string            `json:"label"`
	Pairs []ExplicitPair    `json:"pairs"`
}

type ExplicitPair struct {
	Devices map[string]string `json:"devices"`
	Rail    int               `json:"rail"`
	NUMA    int               `json:"numa"`
}
