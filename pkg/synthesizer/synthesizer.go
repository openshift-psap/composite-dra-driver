// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthesizer

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/openshift-psap/composite-dra-driver/pkg/config"
	"github.com/openshift-psap/composite-dra-driver/pkg/metrics"
	"github.com/openshift-psap/composite-dra-driver/pkg/store"
)

// PreparedDevicesFunc returns underlying devices currently prepared, grouped by composition.
type PreparedDevicesFunc func() map[string][]struct{ SourceName, Device string }

// Synthesizer watches underlying ResourceSlices, computes valid device groupings,
// publishes composite ResourceSlices, and maintains the DeviceStore.
type Synthesizer struct {
	cfg             *config.CompositeConfig
	nodeName        string
	kubeClient      kubernetes.Interface
	watcher         *Watcher
	pairer          *Pairer
	publisher       ResourcePublisher
	store           *store.DeviceStore
	preparedDevices PreparedDevicesFunc
}

func New(cfg *config.CompositeConfig, nodeName string, kubeClient kubernetes.Interface, deviceStore *store.DeviceStore, publisher ResourcePublisher) *Synthesizer {
	nodeLabels := FetchNodeLabels(kubeClient, nodeName)

	s := &Synthesizer{
		cfg:       cfg,
		nodeName:  nodeName,
		kubeClient: kubeClient,
		pairer:    NewPairer(cfg.Sources, cfg.Compositions, nodeLabels),
		publisher: publisher,
		store:     deviceStore,
	}

	sourceDrivers := make([]string, 0, len(cfg.Sources))
	for _, src := range cfg.Sources {
		sourceDrivers = append(sourceDrivers, src.Driver)
	}

	s.watcher = NewWatcher(kubeClient, nodeName, sourceDrivers, s.recompute)

	return s
}

func FetchNodeLabels(kubeClient kubernetes.Interface, nodeName string) map[string]string {
	node, err := kubeClient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "synthesizer: could not fetch node labels", "node", nodeName)
		return nil
	}
	return node.Labels
}

// SetPreparedDevicesFunc sets the function used to query which underlying
// devices are currently prepared. Used for cross-composition device exclusion.
func (s *Synthesizer) SetPreparedDevicesFunc(fn PreparedDevicesFunc) {
	s.preparedDevices = fn
}

// Recompute triggers an immediate recomputation of composite devices.
// Called by the plugin after Prepare/Unprepare to update ResourceSlice pools
// as fast as possible. No debounce — each Prepare/Unprepare is a single call,
// not a burst. Runs in the caller's goroutine (plugin already dispatches via go).
func (s *Synthesizer) Recompute() {
	s.recompute()
}

func (s *Synthesizer) Start(ctx context.Context) error {
	klog.InfoS("synthesizer: starting", "node", s.nodeName)
	return s.watcher.Start(ctx)
}

func (s *Synthesizer) recompute() {
	start := time.Now()

	sourceMap := make(map[string]string, len(s.cfg.Sources))
	for _, src := range s.cfg.Sources {
		sourceMap[src.Name] = src.Driver
	}

	devicesBySource := s.watcher.GetSourceDevices(sourceMap)

	totalDevices := 0
	for name, devs := range devicesBySource {
		totalDevices += len(devs)
		klog.V(2).InfoS("synthesizer: source device count", "source", name, "count", len(devs))
	}

	var preparedByComp map[string][]struct{ SourceName, Device string }
	if s.preparedDevices != nil {
		preparedByComp = s.preparedDevices()
	}

	// Compute capacity (no exclusion) for metrics
	capacityDevices := s.pairer.ComputePairs(devicesBySource)
	capacityByComp := make(map[string]int)
	for _, cd := range capacityDevices {
		capacityByComp[cd.Mapping.CompositionName]++
	}
	for _, comp := range s.cfg.Compositions {
		metrics.SynthesisDevicesCapacity.WithLabelValues(comp.Name).Set(float64(capacityByComp[comp.Name]))
	}

	compositeDevices := s.pairer.ComputePairsWithExclusion(devicesBySource, preparedByComp)
	klog.InfoS("synthesizer: computed composite devices", "count", len(compositeDevices), "sourceDevices", totalDevices)

	countByComposition := make(map[string]int)
	for _, cd := range compositeDevices {
		countByComposition[cd.Mapping.CompositionName]++
	}
	for _, comp := range s.cfg.Compositions {
		metrics.SynthesisDevicesTotal.WithLabelValues(comp.Name).Set(float64(countByComposition[comp.Name]))
	}

	newMappings := make(map[string]*store.DeviceMapping, len(compositeDevices))
	for _, cd := range compositeDevices {
		poolName := PoolName(s.cfg.Driver.Name, s.nodeName, cd.Mapping.CompositionName)
		newMappings[fmt.Sprintf("%s/%s", poolName, cd.Name)] = cd.Mapping
	}
	s.store.ReplaceAll(newMappings)

	if err := s.publisher.Publish(s.cfg.Driver.Name, s.nodeName, compositeDevices); err != nil {
		klog.ErrorS(err, "synthesizer: publish failed")
	}

	elapsed := time.Since(start).Seconds()
	for comp := range countByComposition {
		metrics.SynthesisDurationSeconds.WithLabelValues(comp).Observe(elapsed)
	}
}
