package webhook

import (
	"fmt"

	resourceapi "k8s.io/api/resource/v1"
)

// BuildClaimSpec generates a ResourceClaimSpec targeting the composite DeviceClass
// with NUMA affinity constraints.
//
// For pairCount=8, pairsPerNUMA=4, it produces:
//   - 8 DeviceRequests (pair-0 through pair-7)
//   - 2 MatchAttribute constraints grouping [pair-0..3] and [pair-4..7] by numaNode
//
// The scheduler allocates all 8 from composite ResourceSlices, enforcing that
// each group of 4 lands on the same NUMA zone.
func BuildClaimSpec(deviceClassName string, numaAttribute string, pairCount int, pairsPerNUMA int) resourceapi.ResourceClaimSpec {
	spec := resourceapi.ResourceClaimSpec{}

	if pairsPerNUMA <= 0 {
		pairsPerNUMA = pairCount
	}

	requests := make([]resourceapi.DeviceRequest, pairCount)
	for i := range requests {
		requests[i] = resourceapi.DeviceRequest{
			Name: fmt.Sprintf("pair-%d", i),
			Exactly: &resourceapi.ExactDeviceRequest{
				DeviceClassName: deviceClassName,
				AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
				Count:           1,
			},
		}
	}
	spec.Devices.Requests = requests

	if numaAttribute != "" && pairsPerNUMA < pairCount {
		var constraints []resourceapi.DeviceConstraint
		attr := resourceapi.FullyQualifiedName(numaAttribute)
		for start := 0; start < pairCount; start += pairsPerNUMA {
			end := start + pairsPerNUMA
			if end > pairCount {
				end = pairCount
			}

			group := make([]string, end-start)
			for i := start; i < end; i++ {
				group[i-start] = fmt.Sprintf("pair-%d", i)
			}

			constraints = append(constraints, resourceapi.DeviceConstraint{
				Requests:       group,
				MatchAttribute: &attr,
			})
		}
		spec.Devices.Constraints = constraints
	}

	return spec
}
