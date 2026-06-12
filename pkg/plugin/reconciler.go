// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	resourceclient "k8s.io/client-go/kubernetes/typed/resource/v1"
	"k8s.io/klog/v2"
)

// StartReconciler periodically cleans up orphaned shadow claims whose
// parent composite claim no longer exists or is deallocated.
func StartReconciler(ctx context.Context, client resourceclient.ResourceV1Interface, driverName string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	klog.Infof("reconciler: started (interval=%s)", interval)
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
		klog.Warningf("reconciler: list shadow claims: %v", err)
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
			klog.V(2).Infof("reconciler: deleting orphaned shadow claim %s/%s (composite %s gone)",
				shadow.Namespace, shadow.Name, compositeUID)
			if err := client.ResourceClaims(shadow.Namespace).Delete(ctx, shadow.Name, metav1.DeleteOptions{}); err != nil {
				klog.Warningf("reconciler: delete %s/%s: %v", shadow.Namespace, shadow.Name, err)
			} else {
				orphaned++
			}
		}
	}

	if orphaned > 0 {
		klog.Infof("reconciler: cleaned up %d orphaned shadow claims", orphaned)
	}
}
