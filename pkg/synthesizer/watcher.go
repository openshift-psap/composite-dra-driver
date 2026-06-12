// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthesizer

import (
	"context"
	"sync"
	"time"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	resourcelisters "k8s.io/client-go/listers/resource/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const debounceInterval = 500 * time.Millisecond

// Watcher watches ResourceSlices from underlying drivers and triggers recomputation.
type Watcher struct {
	kubeClient     kubernetes.Interface
	nodeName       string
	sourceDrivers  map[string]bool
	lister         resourcelisters.ResourceSliceLister
	onChange       func()

	mu             sync.Mutex
	debounceTimer  *time.Timer
}

func NewWatcher(kubeClient kubernetes.Interface, nodeName string, sourceDrivers []string, onChange func()) *Watcher {
	drivers := make(map[string]bool, len(sourceDrivers))
	for _, d := range sourceDrivers {
		drivers[d] = true
	}
	return &Watcher{
		kubeClient:    kubeClient,
		nodeName:      nodeName,
		sourceDrivers: drivers,
		onChange:       onChange,
	}
}

func (w *Watcher) Start(ctx context.Context) error {
	factory := informers.NewSharedInformerFactory(w.kubeClient, 30*time.Second)
	sliceInformer := factory.Resource().V1().ResourceSlices()
	w.lister = sliceInformer.Lister()

	sliceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { w.handleEvent(obj) },
		UpdateFunc: func(_, obj interface{}) { w.handleEvent(obj) },
		DeleteFunc: func(obj interface{}) { w.handleEvent(obj) },
	})

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	klog.Infof("watcher: cache synced, triggering initial computation")
	w.onChange()

	<-ctx.Done()
	return nil
}

func (w *Watcher) handleEvent(obj interface{}) {
	slice, ok := obj.(*resourceapi.ResourceSlice)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		slice, ok = tombstone.Obj.(*resourceapi.ResourceSlice)
		if !ok {
			return
		}
	}

	if !w.isRelevantSlice(slice) {
		return
	}

	w.debouncedOnChange()
}

func (w *Watcher) isRelevantSlice(slice *resourceapi.ResourceSlice) bool {
	if slice.Spec.NodeName == nil || *slice.Spec.NodeName != w.nodeName {
		return false
	}
	return w.sourceDrivers[slice.Spec.Driver]
}

func (w *Watcher) debouncedOnChange() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
	}
	w.debounceTimer = time.AfterFunc(debounceInterval, func() {
		klog.V(2).Info("watcher: debounce fired, triggering recomputation")
		w.onChange()
	})
}

// GetSourceDevices returns all devices from underlying driver ResourceSlices on this node.
func (w *Watcher) GetSourceDevices(sources map[string]string) map[string][]SourceDevice {
	result := make(map[string][]SourceDevice)

	allSlices, err := w.lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("watcher: list ResourceSlices: %v", err)
		return result
	}

	driverToSource := make(map[string]string, len(sources))
	for srcName, driverName := range sources {
		driverToSource[driverName] = srcName
	}

	for _, slice := range allSlices {
		if slice.Spec.NodeName == nil || *slice.Spec.NodeName != w.nodeName {
			continue
		}
		srcName, ok := driverToSource[slice.Spec.Driver]
		if !ok {
			continue
		}

		poolName := ""
		if slice.Spec.Pool.Name != "" {
			poolName = slice.Spec.Pool.Name
		}

		for _, dev := range slice.Spec.Devices {
			attrs := make(map[string]resourceapi.DeviceAttribute)
			for qn, attr := range dev.Attributes {
				attrs[string(qn)] = attr
			}

			result[srcName] = append(result[srcName], SourceDevice{
				SourceName: srcName,
				Driver:     slice.Spec.Driver,
				Pool:       poolName,
				DeviceName: dev.Name,
				Attributes: attrs,
			})
		}
	}

	return result
}
