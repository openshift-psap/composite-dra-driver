// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package shadow

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strings"
	"text/template"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

// DeviceParamsResolver resolves opaque driver params for shadow claims
// from an externally-maintained ConfigMap file.
type DeviceParamsResolver struct {
	nodeName   string
	nodeLabels map[string]string
	sources    map[string]*sourceParamsConfig
}

type sourceParamsConfig struct {
	paramsTemplate *template.Template
	entries        []paramEntry
	overrides      []overrideEntry
}

type attributeMatcher struct {
	Prefix string `json:"prefix,omitempty"`
	Exact  string `json:"exact,omitempty"`
}

type paramEntry struct {
	Match  map[string]attributeMatcher `json:"match,omitempty"`
	Values map[string]interface{}      `json:"values,omitempty"`
}

type overrideEntry struct {
	NodeSelector map[string]string `json:"nodeSelector"`
	Entries      []paramEntry      `json:"entries"`
}

type sourceParamsYAML struct {
	Params    string          `json:"params"`
	Entries   []paramEntry    `json:"entries"`
	Overrides []overrideEntry `json:"overrides,omitempty"`
}

type templateData struct {
	PairOrdinal int
	NodeName    string
	DeviceName  string
	SourceName  string
}

func NewDeviceParamsResolver(path, nodeName string, nodeLabels map[string]string) (*DeviceParamsResolver, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read device params file %s: %w", path, err)
	}

	var raw map[string]sourceParamsYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse device params file: %w", err)
	}

	sources := make(map[string]*sourceParamsConfig, len(raw))
	for name, sp := range raw {
		funcMap := template.FuncMap{
			"device":  func(key string) interface{} { return nil },
			"network": networkCIDR,
		}
		tmpl, err := template.New(name).Funcs(funcMap).Parse(sp.Params)
		if err != nil {
			return nil, fmt.Errorf("parse template for source %q: %w", name, err)
		}
		sources[name] = &sourceParamsConfig{
			paramsTemplate: tmpl,
			entries:        sp.Entries,
			overrides:      sp.Overrides,
		}
	}

	return &DeviceParamsResolver{
		nodeName:   nodeName,
		nodeLabels: nodeLabels,
		sources:    sources,
	}, nil
}

// ResolveForDevice returns opaque params JSON for a device, or nil if no match.
func (r *DeviceParamsResolver) ResolveForDevice(
	sourceName string,
	attrs map[string]resourceapi.DeviceAttribute,
	pairOrdinal int,
) []byte {
	src, ok := r.sources[sourceName]
	if !ok {
		return nil
	}

	baseValues := r.findMatchingEntry(src.entries, attrs)
	if baseValues == nil {
		return nil
	}

	merged := copyValues(baseValues)

	for _, override := range src.overrides {
		if !matchesNodeSelector(override.NodeSelector, r.nodeLabels) {
			continue
		}
		overrideValues := r.findMatchingEntryByMatch(override.Entries, attrs, src.entries)
		if overrideValues != nil {
			for k, v := range overrideValues {
				merged[k] = v
			}
		}
	}

	deviceFunc := func(key string) interface{} {
		attr, ok := attrs[key]
		if !ok {
			return nil
		}
		if attr.StringValue != nil {
			return *attr.StringValue
		}
		if attr.IntValue != nil {
			return *attr.IntValue
		}
		if attr.BoolValue != nil {
			return *attr.BoolValue
		}
		return nil
	}

	tmpl, err := src.paramsTemplate.Clone()
	if err != nil {
		klog.ErrorS(err, "params: clone template failed", "source", sourceName)
		return nil
	}
	tmpl.Funcs(template.FuncMap{
		"device":  deviceFunc,
		"network": networkCIDR,
	})

	td := buildTemplateData(merged, pairOrdinal, r.nodeName, sourceName)

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		klog.ErrorS(err, "params: execute template failed", "source", sourceName)
		return nil
	}

	return buf.Bytes()
}

func (r *DeviceParamsResolver) findMatchingEntry(entries []paramEntry, attrs map[string]resourceapi.DeviceAttribute) map[string]interface{} {
	for _, entry := range entries {
		if matchesDevice(entry.Match, attrs) {
			return entry.Values
		}
	}
	return nil
}

// findMatchingEntryByMatch finds override entries that match the same device,
// linking back to base entries by matching the same device attributes.
func (r *DeviceParamsResolver) findMatchingEntryByMatch(overrideEntries []paramEntry, attrs map[string]resourceapi.DeviceAttribute, _ []paramEntry) map[string]interface{} {
	for _, entry := range overrideEntries {
		if matchesDevice(entry.Match, attrs) {
			return entry.Values
		}
	}
	return nil
}

func matchesDevice(match map[string]attributeMatcher, attrs map[string]resourceapi.DeviceAttribute) bool {
	if len(match) == 0 {
		return true
	}
	for attrKey, matcher := range match {
		attrVal, ok := getAttrString(attrs, attrKey)
		if !ok {
			return false
		}
		if matcher.Prefix != "" && !strings.HasPrefix(attrVal, matcher.Prefix) {
			return false
		}
		if matcher.Exact != "" && attrVal != matcher.Exact {
			return false
		}
	}
	return true
}

func getAttrString(attrs map[string]resourceapi.DeviceAttribute, key string) (string, bool) {
	attr, ok := attrs[key]
	if !ok {
		return "", false
	}
	if attr.StringValue != nil {
		return *attr.StringValue, true
	}
	if attr.IntValue != nil {
		return fmt.Sprintf("%d", *attr.IntValue), true
	}
	if attr.BoolValue != nil {
		return fmt.Sprintf("%t", *attr.BoolValue), true
	}
	return "", false
}

func matchesNodeSelector(selector, labels map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func copyValues(src map[string]interface{}) map[string]interface{} {
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// buildTemplateData creates the template execution context.
// External values are set as top-level fields via a map so templates
// can access them as {{.Gateway}}, {{.Table}}, etc.
func buildTemplateData(values map[string]interface{}, pairOrdinal int, nodeName, sourceName string) map[string]interface{} {
	data := make(map[string]interface{}, len(values)+4)
	for k, v := range values {
		data[k] = v
	}
	data["PairOrdinal"] = pairOrdinal
	data["NodeName"] = nodeName
	data["SourceName"] = sourceName
	return data
}

// networkCIDR extracts the network CIDR from an IP/mask string.
// e.g., "172.16.1.1/24" → "172.16.1.0/24"
func networkCIDR(ipCIDR string) string {
	_, ipNet, err := net.ParseCIDR(ipCIDR)
	if err != nil {
		klog.V(2).InfoS("params: networkCIDR parse failed", "expression", ipCIDR, "err", err)
		return ipCIDR
	}
	return ipNet.String()
}
