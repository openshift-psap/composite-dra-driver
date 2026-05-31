package synthesizer

import (
	"fmt"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
)

const maxDevicesPerSlice = 128

// ResourcePublisher is the interface for publishing composite ResourceSlices.
type ResourcePublisher interface {
	Publish(driverName, nodeName string, compositeDevices []CompositeDevice) error
}

// HelperPublisher publishes via kubeletplugin.Helper.PublishResources().
type HelperPublisher struct {
	publishFn func(resources resourceslice.DriverResources) error
}

func NewHelperPublisher(publishFn func(resources resourceslice.DriverResources) error) *HelperPublisher {
	return &HelperPublisher{publishFn: publishFn}
}

func (p *HelperPublisher) Publish(driverName, nodeName string, compositeDevices []CompositeDevice) error {
	poolName := fmt.Sprintf("%s-%s", driverName, nodeName)

	var slices []resourceslice.Slice
	for i := 0; i < len(compositeDevices); i += maxDevicesPerSlice {
		end := i + maxDevicesPerSlice
		if end > len(compositeDevices) {
			end = len(compositeDevices)
		}
		batch := compositeDevices[i:end]

		devices := make([]resourceapi.Device, 0, len(batch))
		for _, cd := range batch {
			devices = append(devices, resourceapi.Device{
				Name:       cd.Name,
				Attributes: convertAttributes(cd.Attributes),
			})
		}
		slices = append(slices, resourceslice.Slice{Devices: devices})
	}

	if len(slices) == 0 {
		slices = []resourceslice.Slice{{}}
	}

	resources := resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			poolName: {Slices: slices},
		},
	}

	klog.Infof("publisher: publishing %d devices in %d slice(s) for pool %s",
		len(compositeDevices), len(slices), poolName)

	return p.publishFn(resources)
}

// LogPublisher is a stub that logs instead of publishing (for testing).
type LogPublisher struct{}

func (p *LogPublisher) Publish(driverName, nodeName string, compositeDevices []CompositeDevice) error {
	poolName := fmt.Sprintf("%s-%s", driverName, nodeName)
	klog.Infof("publisher(log): %d devices for pool %s", len(compositeDevices), poolName)
	return nil
}

// BuildResourceSlices is kept for unit testing — builds specs without publishing.
func BuildResourceSlices(driverName, nodeName string, compositeDevices []CompositeDevice) []resourceapi.ResourceSliceSpec {
	if len(compositeDevices) == 0 {
		return nil
	}

	poolName := fmt.Sprintf("%s-%s", driverName, nodeName)

	var sliceSpecs []resourceapi.ResourceSliceSpec
	for i := 0; i < len(compositeDevices); i += maxDevicesPerSlice {
		end := i + maxDevicesPerSlice
		if end > len(compositeDevices) {
			end = len(compositeDevices)
		}
		batch := compositeDevices[i:end]

		devices := make([]resourceapi.Device, 0, len(batch))
		for _, cd := range batch {
			devices = append(devices, resourceapi.Device{
				Name:       cd.Name,
				Attributes: convertAttributes(cd.Attributes),
			})
		}

		spec := resourceapi.ResourceSliceSpec{
			Driver:   driverName,
			NodeName: &nodeName,
			Pool: resourceapi.ResourcePool{
				Name:               poolName,
				ResourceSliceCount: 1,
			},
			Devices: devices,
		}
		sliceSpecs = append(sliceSpecs, spec)
	}

	for i := range sliceSpecs {
		sliceSpecs[i].Pool.ResourceSliceCount = int64(len(sliceSpecs))
	}

	return sliceSpecs
}

func convertAttributes(attrs map[string]resourceapi.DeviceAttribute) map[resourceapi.QualifiedName]resourceapi.DeviceAttribute {
	result := make(map[resourceapi.QualifiedName]resourceapi.DeviceAttribute, len(attrs))
	for key, val := range attrs {
		result[resourceapi.QualifiedName(key)] = val
	}
	return result
}

