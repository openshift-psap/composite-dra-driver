// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"fmt"

	resourceapi "k8s.io/api/resource/v1"
)

// BuildClaimSpec generates a ResourceClaimSpec targeting the composite DeviceClass.
//
// For pairCount=4, it produces 4 DeviceRequests (pair-0 through pair-3).
// No NUMA constraints — scheduler picks devices freely.
func BuildClaimSpec(deviceClassName string, pairCount int) resourceapi.ResourceClaimSpec {
	spec := resourceapi.ResourceClaimSpec{}

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

	return spec
}
