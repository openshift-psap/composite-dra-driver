// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	resourceclient "k8s.io/client-go/kubernetes/typed/resource/v1"
	"k8s.io/klog/v2"

	"github.com/openshift-psap/composite-dra-driver/pkg/metrics"
)

// StartReconciler periodically cleans up orphaned shadow claims whose
// parent composite claim no longer exists or is deallocated.
func StartReconciler(ctx context.Context, client resourceclient.ResourceV1Interface, driverName string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	klog.InfoS("reconciler: started", "interval", interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcileOrphans(ctx, client, driverName)
		}
	}
}

func reconcileOrphans(ctx context.Context, client resourceclient.ResourceV1Interface, driverName string) {
	labelSelector := "app.kubernetes.io/managed-by=" + driverName

	namespaces := []string{""}
	claims, err := client.ResourceClaims("").List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		klog.ErrorS(err, "reconciler: list shadow claims failed")
		return
	}
	_ = namespaces

	orphaned := 0
	for _, shadow := range claims.Items {
		compositeUID := shadow.Labels["composite-claim-uid"]
		if compositeUID == "" {
			continue
		}

		ownerExists := false
		for _, ref := range shadow.OwnerReferences {
			if string(ref.UID) == compositeUID {
				_, err := client.ResourceClaims(shadow.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
				if err == nil {
					ownerExists = true
				}
				break
			}
		}

		if !ownerExists {
			klog.V(2).InfoS("reconciler: deleting orphaned shadow claim", "namespace", shadow.Namespace, "name", shadow.Name, "compositeUID", compositeUID)
			if err := client.ResourceClaims(shadow.Namespace).Delete(ctx, shadow.Name, metav1.DeleteOptions{}); err != nil {
				klog.ErrorS(err, "reconciler: delete shadow claim failed", "namespace", shadow.Namespace, "name", shadow.Name)
			} else {
				orphaned++
			}
		}
	}

	if orphaned > 0 {
		metrics.ReconcilerClaimsCleanedTotal.Add(float64(orphaned))
		klog.InfoS("reconciler: cleaned up orphaned shadow claims", "count", orphaned)
	}
}
