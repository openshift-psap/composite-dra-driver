// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthesizer

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/openshift-psap/composite-dra-driver/pkg/config"
	"github.com/openshift-psap/composite-dra-driver/pkg/store"
)

// Synthesizer watches underlying ResourceSlices, computes valid device groupings,
// publishes composite ResourceSlices, and maintains the DeviceStore.
type Synthesizer struct {
	cfg        *config.CompositeConfig
	nodeName   string
	kubeClient kubernetes.Interface
	watcher    *Watcher
	pairer     *Pairer
	publisher  ResourcePublisher
	store      *store.DeviceStore
}

func New(cfg *config.CompositeConfig, nodeName string, kubeClient kubernetes.Interface, deviceStore *store.DeviceStore, publisher ResourcePublisher) *Synthesizer {
	nodeLabels := fetchNodeLabels(kubeClient, nodeName)

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

func fetchNodeLabels(kubeClient kubernetes.Interface, nodeName string) map[string]string {
	node, err := kubeClient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		klog.Warningf("synthesizer: could not fetch node labels for %s: %v", nodeName, err)
		return nil
	}
	return node.Labels
}

func (s *Synthesizer) Start(ctx context.Context) error {
	klog.Infof("synthesizer: starting for node %s", s.nodeName)
	return s.watcher.Start(ctx)
}

func (s *Synthesizer) recompute() {
	sourceMap := make(map[string]string, len(s.cfg.Sources))
	for _, src := range s.cfg.Sources {
		sourceMap[src.Name] = src.Driver
	}

	devicesBySource := s.watcher.GetSourceDevices(sourceMap)

	totalDevices := 0
	for name, devs := range devicesBySource {
		totalDevices += len(devs)
		klog.V(2).Infof("synthesizer: source %s has %d devices", name, len(devs))
	}

	compositeDevices := s.pairer.ComputePairs(devicesBySource)
	klog.Infof("synthesizer: computed %d composite devices from %d source devices", len(compositeDevices), totalDevices)

	newMappings := make(map[string]*store.DeviceMapping, len(compositeDevices))
	for _, cd := range compositeDevices {
		poolName := PoolName(s.cfg.Driver.Name, s.nodeName, cd.Mapping.CompositionName)
		newMappings[fmt.Sprintf("%s/%s", poolName, cd.Name)] = cd.Mapping
	}
	s.store.ReplaceAll(newMappings)

	if err := s.publisher.Publish(s.cfg.Driver.Name, s.nodeName, compositeDevices); err != nil {
		klog.Errorf("synthesizer: publish failed: %v", err)
	}
}
