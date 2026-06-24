// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthesizer

import (
	"fmt"
	"sort"
	"strings"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/klog/v2"

	"github.com/openshift-psap/composite-dra-driver/pkg/config"
	"github.com/openshift-psap/composite-dra-driver/pkg/store"
)

// SourceDevice is a device from an underlying driver's ResourceSlice.
type SourceDevice struct {
	SourceName string
	Driver     string
	Pool       string
	DeviceName string
	DeviceClassName string
	Attributes map[string]resourceapi.DeviceAttribute
}

// CompositeDevice is a generated composite device ready for publishing.
type CompositeDevice struct {
	Name       string
	Attributes map[string]resourceapi.DeviceAttribute
	Mapping    *store.DeviceMapping
}

// Pairer computes valid device groupings from source devices according to composition rules.
type Pairer struct {
	sources      map[string]*config.SourceConfig
	compositions []config.CompositionConfig
	celFilter    *CELFilter
	nodeLabels   map[string]string
}

func NewPairer(sources []config.SourceConfig, compositions []config.CompositionConfig, nodeLabels map[string]string) *Pairer {
	srcMap := make(map[string]*config.SourceConfig, len(sources))
	for i := range sources {
		srcMap[sources[i].Name] = &sources[i]
	}
	celFilter, err := NewCELFilter()
	if err != nil {
		klog.ErrorS(err, "pairer: CEL filter init failed, filters disabled")
	}
	return &Pairer{
		sources:      srcMap,
		compositions: compositions,
		celFilter:    celFilter,
		nodeLabels:   nodeLabels,
	}
}

// ComputePairs takes all source devices on a node and returns valid composite devices.
func (p *Pairer) ComputePairs(devicesBySource map[string][]SourceDevice) []CompositeDevice {
	return p.ComputePairsWithExclusion(devicesBySource, nil)
}

// ComputePairsWithExclusion computes composite devices, excluding underlying
// devices that are prepared by other compositions. For each composition, devices
// prepared by OTHER compositions are filtered out before pairing.
func (p *Pairer) ComputePairsWithExclusion(devicesBySource map[string][]SourceDevice, preparedByComp map[string][]struct{ SourceName, Device string }) []CompositeDevice {
	var result []CompositeDevice
	for _, comp := range p.compositions {
		available := devicesBySource
		if preparedByComp != nil {
			available = p.excludePreparedByOthers(devicesBySource, preparedByComp, comp.Name)
		}
		pairs := p.computeForComposition(comp, available)
		result = append(result, pairs...)
	}
	return result
}

// excludePreparedByOthers returns a filtered copy of devicesBySource that
// removes devices prepared by compositions other than currentComp.
func (p *Pairer) excludePreparedByOthers(devicesBySource map[string][]SourceDevice, preparedByComp map[string][]struct{ SourceName, Device string }, currentComp string) map[string][]SourceDevice {
	excluded := make(map[string]map[string]bool)
	for compName, devs := range preparedByComp {
		if compName == currentComp {
			continue
		}
		for _, d := range devs {
			if excluded[d.SourceName] == nil {
				excluded[d.SourceName] = make(map[string]bool)
			}
			excluded[d.SourceName][d.Device] = true
		}
	}

	if len(excluded) == 0 {
		return devicesBySource
	}

	filtered := make(map[string][]SourceDevice, len(devicesBySource))
	for source, devices := range devicesBySource {
		excl := excluded[source]
		if excl == nil {
			filtered[source] = devices
			continue
		}
		for _, dev := range devices {
			if !excl[dev.DeviceName] {
				filtered[source] = append(filtered[source], dev)
			}
		}
	}
	return filtered
}

func (p *Pairer) computeForComposition(comp config.CompositionConfig, devicesBySource map[string][]SourceDevice) []CompositeDevice {
	filtered := make(map[string][]SourceDevice)
	for _, member := range comp.Members {
		devices := devicesBySource[member.Source]
		if f, ok := comp.Filters[member.Source]; ok {
			devices = p.applyFilter(devices, f)
		}
		filtered[member.Source] = devices
	}

	if comp.PairingMode == "explicit" {
		return p.pairWithExplicit(comp, filtered)
	}

	if len(comp.Constraints) == 0 {
		return p.pairSingleSource(comp, filtered)
	}

	return p.pairWithMatchAttribute(comp, filtered)
}

// pairWithMatchAttribute groups devices by shared constraint attribute values,
// then generates combinations within each group.
func (p *Pairer) pairWithMatchAttribute(comp config.CompositionConfig, devicesBySource map[string][]SourceDevice) []CompositeDevice {
	constraint := comp.Constraints[0]

	type groupKey struct {
		sourceName string
		attrValue  string
	}

	groups := make(map[string]map[string][]SourceDevice)

	for _, member := range comp.Members {
		for _, dev := range devicesBySource[member.Source] {
			val, ok := getAttributeString(dev.Attributes, constraint.Attribute)
			if !ok {
				continue
			}
			if groups[val] == nil {
				groups[val] = make(map[string][]SourceDevice)
			}
			groups[val][member.Source] = append(groups[val][member.Source], dev)
		}
	}

	var result []CompositeDevice
	for _, sourcesInGroup := range groups {
		allPresent := true
		for _, member := range comp.Members {
			if len(sourcesInGroup[member.Source]) < member.Count {
				allPresent = false
				break
			}
		}
		if !allPresent {
			continue
		}

		combos := generateCombinations(comp.Members, sourcesInGroup)
		for _, combo := range combos {
			cd := p.buildCompositeDevice(comp, combo)
			result = append(result, cd)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// pairSingleSource generates composite devices from a single-source composition,
// respecting member count via C(n,k) combinations. Validation ensures this is
// only reached when the composition has one unique source.
func (p *Pairer) pairSingleSource(comp config.CompositionConfig, devicesBySource map[string][]SourceDevice) []CompositeDevice {
	combos := generateCombinations(comp.Members, devicesBySource)
	var result []CompositeDevice
	for _, combo := range combos {
		cd := p.buildCompositeDevice(comp, combo)
		result = append(result, cd)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func (p *Pairer) pairWithExplicit(comp config.CompositionConfig, devicesBySource map[string][]SourceDevice) []CompositeDevice {
	if p.celFilter == nil {
		klog.ErrorS(nil, "pairer: explicit pairing requires CEL filter but it is nil")
		return nil
	}

	nodePoolValue := p.nodeLabels[comp.NodePoolLabelKey]
	var pool *config.ExplicitNodePool
	for i := range comp.NodePools {
		if comp.NodePools[i].Label == nodePoolValue {
			pool = &comp.NodePools[i]
			break
		}
	}
	if pool == nil {
		klog.V(2).InfoS("pairer: explicit mode: no pool matches node label", "label", comp.NodePoolLabelKey, "value", nodePoolValue)
		return nil
	}

	consumed := make(map[string]map[string]bool)
	for _, member := range comp.Members {
		consumed[member.Source] = make(map[string]bool)
	}

	var result []CompositeDevice
	for i, ep := range pool.Pairs {
		combo := make(map[string][]SourceDevice)
		valid := true

		for _, member := range comp.Members {
			sel, ok := ep.Selectors[member.Source]
			if !ok {
				klog.ErrorS(nil, "pairer: explicit pair missing selector", "pair", i, "source", member.Source)
				valid = false
				break
			}

			var matched []SourceDevice
			for _, dev := range devicesBySource[member.Source] {
				if consumed[member.Source][dev.DeviceName] {
					continue
				}
				if p.celFilter.Match(sel, dev.Attributes) {
					matched = append(matched, dev)
				}
			}

			if len(matched) < member.Count {
				klog.V(2).InfoS("pairer: explicit pair insufficient matches", "pair", i, "source", member.Source, "matched", len(matched), "needed", member.Count)
				valid = false
				break
			}
			if len(matched) > member.Count {
				klog.ErrorS(nil, "pairer: explicit pair selector matched too many devices, using first N — use a more specific selector for deterministic pairing", "pair", i, "source", member.Source, "matched", len(matched), "needed", member.Count)
			}

			combo[member.Source] = matched[:member.Count]
		}

		if !valid {
			continue
		}

		for src, devs := range combo {
			for _, dev := range devs {
				consumed[src][dev.DeviceName] = true
			}
		}

		cd := p.buildCompositeDevice(comp, combo)
		cd.Mapping.RailIndex = ep.Rail
		cd.Mapping.NUMANode = ep.NUMA
		numaVal := int64(ep.NUMA)
		cd.Attributes["composite/numaNode"] = resourceapi.DeviceAttribute{IntValue: &numaVal}
		railVal := int64(ep.Rail)
		cd.Attributes["composite/railIndex"] = resourceapi.DeviceAttribute{IntValue: &railVal}

		result = append(result, cd)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	klog.InfoS("pairer: explicit mode produced pairs", "count", len(result), "total", len(pool.Pairs), "pool", pool.Label)
	return result
}

// generateCombinations produces all valid device selections respecting member counts.
// Each combo maps source name → selected devices.
func generateCombinations(members []config.MemberConfig, available map[string][]SourceDevice) []map[string][]SourceDevice {
	if len(members) == 0 {
		return []map[string][]SourceDevice{{}}
	}

	member := members[0]
	rest := members[1:]
	avail := available[member.Source]

	if len(avail) < member.Count {
		return nil
	}

	var results []map[string][]SourceDevice
	indices := chooseCombinations(len(avail), member.Count)
	for _, idxSet := range indices {
		selected := make([]SourceDevice, len(idxSet))
		for i, idx := range idxSet {
			selected[i] = avail[idx]
		}
		subCombos := generateCombinations(rest, available)
		for _, sub := range subCombos {
			combo := make(map[string][]SourceDevice, len(sub)+1)
			for k, v := range sub {
				combo[k] = v
			}
			combo[member.Source] = selected
			results = append(results, combo)
		}
	}
	return results
}

// chooseCombinations returns all C(n, k) index combinations.
func chooseCombinations(n, k int) [][]int {
	if k > n || k <= 0 {
		return nil
	}
	if k == 1 {
		result := make([][]int, n)
		for i := 0; i < n; i++ {
			result[i] = []int{i}
		}
		return result
	}

	var results [][]int
	combo := make([]int, k)
	var generate func(start, depth int)
	generate = func(start, depth int) {
		if depth == k {
			c := make([]int, k)
			copy(c, combo)
			results = append(results, c)
			return
		}
		for i := start; i <= n-(k-depth); i++ {
			combo[depth] = i
			generate(i+1, depth+1)
		}
	}
	generate(0, 0)
	return results
}

func (p *Pairer) buildCompositeDevice(comp config.CompositionConfig, combo map[string][]SourceDevice) CompositeDevice {
	attrs := make(map[string]resourceapi.DeviceAttribute)
	var members []store.DeviceMember
	var nameParts []string

	for _, memberCfg := range comp.Members {
		src := p.sources[memberCfg.Source]
		for i, dev := range combo[memberCfg.Source] {
			suffix := ""
			if memberCfg.Count > 1 {
				suffix = fmt.Sprintf("-%d", i)
			}
			prefix := fmt.Sprintf("%s%s", src.Name, suffix)

			for _, ag := range src.ForwardAttributes {
				for _, attrName := range ag.Attributes {
					fullKey := qualifiedAttrName(ag.Domain, attrName)
					if val, ok := dev.Attributes[fullKey]; ok {
						compositeKey := fmt.Sprintf("%s/%s", prefix, attrName)
						attrs[compositeKey] = val
					}
				}
			}

			nameParts = append(nameParts, sanitizeName(dev.DeviceName))

			members = append(members, store.DeviceMember{
				SourceName:     src.Name,
				Driver:         src.Driver,
				Pool:           dev.Pool,
				Device:         dev.DeviceName,
				DeviceClassName: src.DeviceClassName,
				Attributes:     dev.Attributes,
			})
		}
	}

	for _, c := range comp.Constraints {
		if c.Type == "matchAttribute" {
			if first := combo[comp.Members[0].Source]; len(first) > 0 {
				if val, ok := first[0].Attributes[c.Attribute]; ok {
					attrs[c.Attribute] = val
				}
			}
		}
	}

	numaNode := detectNUMANode(combo)
	if numaNode >= 0 {
		intVal := int64(numaNode)
		attrs["composite/numaNode"] = resourceapi.DeviceAttribute{IntValue: &intVal}
	}

	compNameVal := comp.Name
	attrs["composite/compositionName"] = resourceapi.DeviceAttribute{StringValue: &compNameVal}

	name := strings.Join(nameParts, "--")
	if len(name) > 63 {
		name = name[:63]
	}

	return CompositeDevice{
		Name:       name,
		Attributes: attrs,
		Mapping: &store.DeviceMapping{
			CompositionName: comp.Name,
			Members:         members,
			NUMANode:        numaNode,
		},
	}
}

func detectNUMANode(combo map[string][]SourceDevice) int {
	for _, devs := range combo {
		for _, dev := range devs {
			for key, attr := range dev.Attributes {
				if strings.HasSuffix(key, "numaNode") && attr.IntValue != nil {
					return int(*attr.IntValue)
				}
			}
		}
	}
	return -1
}

func getAttributeString(attrs map[string]resourceapi.DeviceAttribute, fullKey string) (string, bool) {
	attr, ok := attrs[fullKey]
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

func qualifiedAttrName(domain, name string) string {
	if domain == "" {
		return name
	}
	return fmt.Sprintf("%s/%s", domain, name)
}

func sanitizeName(s string) string {
	s = strings.ReplaceAll(s, ":", "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ".", "-")
	return strings.ToLower(s)
}

func (p *Pairer) applyFilter(devices []SourceDevice, f config.FilterConfig) []SourceDevice {
	if f.CEL == "" || p.celFilter == nil {
		return devices
	}
	var filtered []SourceDevice
	for _, dev := range devices {
		if p.celFilter.Match(f.CEL, dev.Attributes) {
			filtered = append(filtered, dev)
		}
	}
	klog.V(2).InfoS("pairer: CEL filter applied", "expression", f.CEL, "passed", len(filtered), "total", len(devices))
	return filtered
}
