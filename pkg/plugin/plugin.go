package plugin

import (
	"context"
	"fmt"
	"sync"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	"github.com/openshift-psap/composite-dra-driver/pkg/shadow"
	"github.com/openshift-psap/composite-dra-driver/pkg/store"
)

// CompositePlugin implements kubeletplugin.DRAPlugin for the composite driver.
type CompositePlugin struct {
	driverName   string
	deviceStore  *store.DeviceStore
	claimMgr     *shadow.ClaimManager
	railResolver *shadow.RailConfigResolver
	grpcClient   *GRPCClient
	stateStore   *store.StateStore

	// tracks shadow claims created per composite claim for cleanup
	mu           sync.Mutex
	shadowClaims map[types.UID][]shadowRecord
}

type shadowRecord struct {
	driverName string
	info       *shadow.ShadowClaimInfo
}

var _ kubeletplugin.DRAPlugin = (*CompositePlugin)(nil)

func NewCompositePlugin(
	driverName string,
	deviceStore *store.DeviceStore,
	claimMgr *shadow.ClaimManager,
	railResolver *shadow.RailConfigResolver,
	grpcClient *GRPCClient,
	stateStore *store.StateStore,
) *CompositePlugin {
	p := &CompositePlugin{
		driverName:   driverName,
		deviceStore:  deviceStore,
		claimMgr:     claimMgr,
		railResolver: railResolver,
		grpcClient:   grpcClient,
		stateStore:   stateStore,
		shadowClaims: make(map[types.UID][]shadowRecord),
	}

	if stateStore != nil {
		p.restoreFromState()
	}

	return p
}

func (p *CompositePlugin) PrepareResourceClaims(
	ctx context.Context,
	claims []*resourceapi.ResourceClaim,
) (map[types.UID]kubeletplugin.PrepareResult, error) {
	results := make(map[types.UID]kubeletplugin.PrepareResult)
	for _, claim := range claims {
		devices, err := p.prepareClaim(ctx, claim)
		results[claim.UID] = kubeletplugin.PrepareResult{Devices: devices, Err: err}
	}
	return results, nil
}

func (p *CompositePlugin) UnprepareResourceClaims(
	ctx context.Context,
	claims []kubeletplugin.NamespacedObject,
) (map[types.UID]error, error) {
	results := make(map[types.UID]error)
	for _, claim := range claims {
		results[claim.UID] = p.unprepareClaim(ctx, claim)
	}
	return results, nil
}

func (p *CompositePlugin) HandleError(ctx context.Context, err error, msg string) {
	runtime.HandleErrorWithContext(ctx, err, msg)
}

// memberWork holds the inputs and outputs for one member's parallel prepare.
type memberWork struct {
	pairIdx     int
	memberIdx   int
	member      store.DeviceMember
	allocResult resourceapi.DeviceRequestAllocationResult
	opaqueConfig []byte

	shadow   shadowRecord
	cdiIDs   []string
	err      error
}

func (p *CompositePlugin) prepareClaim(
	ctx context.Context,
	claim *resourceapi.ResourceClaim,
) ([]kubeletplugin.Device, error) {
	if claim.Status.Allocation == nil {
		return nil, fmt.Errorf("claim %s/%s not allocated", claim.Namespace, claim.Name)
	}

	// Collect all work items upfront
	var work []*memberWork
	pairOrdinal := 0

	for _, allocResult := range claim.Status.Allocation.Devices.Results {
		if allocResult.Driver != p.driverName {
			continue
		}

		mapping := p.deviceStore.Get(allocResult.Pool, allocResult.Device)
		if mapping == nil {
			return nil, fmt.Errorf("unknown composite device %s/%s", allocResult.Pool, allocResult.Device)
		}

		for memberIdx, member := range mapping.Members {
			var opaqueConfig []byte
			if p.railResolver != nil {
				opaqueConfig, _ = p.railResolver.ResolveForDevice(member.Attributes, pairOrdinal)
			}
			work = append(work, &memberWork{
				pairIdx:      pairOrdinal,
				memberIdx:    memberIdx,
				member:       member,
				allocResult:  allocResult,
				opaqueConfig: opaqueConfig,
			})
		}
		pairOrdinal++
	}

	// Phase 1: Create all shadow claims in parallel
	var wg sync.WaitGroup
	for _, w := range work {
		wg.Add(1)
		go func(w *memberWork) {
			defer wg.Done()
			shadowInfo, err := p.claimMgr.Create(ctx, claim, &w.member, w.allocResult.Request, w.opaqueConfig)
			if err != nil {
				if errors.IsAlreadyExists(err) {
					klog.V(2).Infof("plugin: shadow claim already exists for %s/%s (idempotent)", w.member.Driver, w.member.Device)
				} else {
					w.err = fmt.Errorf("create shadow for %s/%s: %w", w.member.Driver, w.member.Device, err)
					return
				}
			}
			w.shadow = shadowRecord{driverName: w.member.Driver, info: shadowInfo}
		}(w)
	}
	wg.Wait()

	// Check for creation errors
	var shadows []shadowRecord
	for _, w := range work {
		if w.err != nil {
			p.cleanupShadows(ctx, shadows)
			return nil, w.err
		}
		shadows = append(shadows, w.shadow)
	}

	// Phase 2: Call gRPC prepare on all underlying drivers in parallel
	for _, w := range work {
		wg.Add(1)
		go func(w *memberWork) {
			defer wg.Done()
			resp, err := p.grpcClient.Prepare(ctx, w.shadow.driverName, w.shadow.info)
			if err != nil {
				w.err = fmt.Errorf("prepare %s via gRPC: %w", w.shadow.driverName, err)
				return
			}
			for _, dev := range resp.Devices {
				w.cdiIDs = append(w.cdiIDs, dev.CdiDeviceIds...)
			}
		}(w)
	}
	wg.Wait()

	// Check for prepare errors
	for _, w := range work {
		if w.err != nil {
			p.cleanupShadows(ctx, shadows)
			return nil, w.err
		}
	}

	// Assemble results grouped by composite device
	devicesByPair := make(map[int]*kubeletplugin.Device)
	for _, w := range work {
		key := w.pairIdx
		dev, ok := devicesByPair[key]
		if !ok {
			dev = &kubeletplugin.Device{
				Requests: []string{w.allocResult.Request},
				PoolName: w.allocResult.Pool,
				DeviceName: w.allocResult.Device,
			}
			devicesByPair[key] = dev
		}
		dev.CDIDeviceIDs = append(dev.CDIDeviceIDs, w.cdiIDs...)
	}

	var allDevices []kubeletplugin.Device
	for i := 0; i < pairOrdinal; i++ {
		if dev, ok := devicesByPair[i]; ok {
			allDevices = append(allDevices, *dev)
		}
	}

	p.mu.Lock()
	p.shadowClaims[claim.UID] = shadows
	p.mu.Unlock()

	p.persistShadows(claim, shadows)

	klog.Infof("plugin: prepared claim %s/%s — %d composite devices, %d shadow claims",
		claim.Namespace, claim.Name, len(allDevices), len(shadows))

	return allDevices, nil
}

func (p *CompositePlugin) unprepareClaim(
	ctx context.Context,
	claim kubeletplugin.NamespacedObject,
) error {
	p.mu.Lock()
	shadows := p.shadowClaims[claim.UID]
	delete(p.shadowClaims, claim.UID)
	p.mu.Unlock()

	var errs []error
	for _, sr := range shadows {
		if err := p.grpcClient.Unprepare(ctx, sr.driverName, sr.info); err != nil {
			klog.Warningf("plugin: unprepare %s for shadow %s: %v", sr.driverName, sr.info.Name, err)
			errs = append(errs, err)
		}
		if err := p.claimMgr.Delete(ctx, sr.info.Namespace, sr.info.Name); err != nil {
			klog.Warningf("plugin: delete shadow claim %s: %v", sr.info.Name, err)
			errs = append(errs, err)
		}
	}

	if len(shadows) == 0 {
		if err := p.claimMgr.DeleteForCompositeClaim(ctx, claim.Namespace, string(claim.UID)); err != nil {
			klog.Warningf("plugin: cleanup orphaned shadows for %s: %v", claim.UID, err)
		}
	}

	p.deleteShadowState(string(claim.UID))

	if len(errs) > 0 {
		return fmt.Errorf("%d errors during unprepare: %v", len(errs), errs)
	}

	klog.Infof("plugin: unprepared claim %s/%s — cleaned up %d shadow claims",
		claim.Namespace, claim.Name, len(shadows))
	return nil
}

func (p *CompositePlugin) cleanupShadows(ctx context.Context, shadows []shadowRecord) {
	for _, sr := range shadows {
		_ = p.grpcClient.Unprepare(ctx, sr.driverName, sr.info)
		_ = p.claimMgr.Delete(ctx, sr.info.Namespace, sr.info.Name)
	}
}

func (p *CompositePlugin) persistShadows(claim *resourceapi.ResourceClaim, shadows []shadowRecord) {
	if p.stateStore == nil {
		return
	}
	entries := make([]store.ShadowEntry, len(shadows))
	for i, sr := range shadows {
		entries[i] = store.ShadowEntry{
			DriverName: sr.driverName,
			Namespace:  sr.info.Namespace,
			Name:       sr.info.Name,
			UID:        sr.info.UID,
		}
	}
	if err := p.stateStore.SaveShadows(store.ShadowRecord{
		CompositeClaimUID: string(claim.UID),
		Namespace:         claim.Namespace,
		Shadows:           entries,
	}); err != nil {
		klog.Warningf("plugin: persist shadow state for %s: %v", claim.UID, err)
	}
}

func (p *CompositePlugin) deleteShadowState(compositeClaimUID string) {
	if p.stateStore == nil {
		return
	}
	if err := p.stateStore.DeleteShadows(compositeClaimUID); err != nil {
		klog.Warningf("plugin: delete shadow state for %s: %v", compositeClaimUID, err)
	}
}

func (p *CompositePlugin) restoreFromState() {
	records, err := p.stateStore.ListAll()
	if err != nil {
		klog.Warningf("plugin: restore state: %v", err)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, rec := range records {
		uid := types.UID(rec.CompositeClaimUID)
		var shadows []shadowRecord
		for _, entry := range rec.Shadows {
			shadows = append(shadows, shadowRecord{
				driverName: entry.DriverName,
				info: &shadow.ShadowClaimInfo{
					Namespace: entry.Namespace,
					Name:      entry.Name,
					UID:       entry.UID,
				},
			})
		}
		p.shadowClaims[uid] = shadows
	}
	klog.Infof("plugin: restored %d shadow claim records from state", len(records))
}
