// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	resourceclient "k8s.io/client-go/kubernetes/typed/resource/v1"
	"k8s.io/klog/v2"

	"github.com/openshift-psap/composite-dra-driver/pkg/metrics"
)

const (
	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedByValue = "composite-dra-webhook"
)

// StartTemplateReconciler periodically deletes orphaned ResourceClaimTemplates
// created by the webhook whose owning pod no longer exists.
func StartTemplateReconciler(ctx context.Context, resourceClient resourceclient.ResourceV1Interface, coreClient corev1client.CoreV1Interface, interval, gracePeriod time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	klog.InfoS("template-reconciler: started", "interval", interval, "gracePeriod", gracePeriod)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcileTemplates(ctx, resourceClient, coreClient, gracePeriod)
		}
	}
}

func reconcileTemplates(ctx context.Context, resourceClient resourceclient.ResourceV1Interface, coreClient corev1client.CoreV1Interface, gracePeriod time.Duration) {
	templates, err := resourceClient.ResourceClaimTemplates("").List(ctx, metav1.ListOptions{
		LabelSelector: ManagedByLabel + "=" + ManagedByValue,
	})
	if err != nil {
		klog.ErrorS(err, "template-reconciler: list templates failed")
		return
	}

	if len(templates.Items) == 0 {
		return
	}

	// Group templates by namespace
	type templatesByNS = map[string][]int
	byNamespace := make(templatesByNS)
	for i, t := range templates.Items {
		byNamespace[t.Namespace] = append(byNamespace[t.Namespace], i)
	}

	orphaned := 0
	now := time.Now()

	for ns, indices := range byNamespace {
		pods, err := coreClient.Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			klog.ErrorS(err, "template-reconciler: list pods failed", "namespace", ns)
			continue
		}

		// Build set of template names referenced by live pods
		referenced := make(map[string]bool)
		for _, pod := range pods.Items {
			for _, claim := range pod.Spec.ResourceClaims {
				if claim.ResourceClaimTemplateName != nil {
					referenced[*claim.ResourceClaimTemplateName] = true
				}
			}
		}

		for _, idx := range indices {
			t := &templates.Items[idx]
			if referenced[t.Name] {
				continue
			}
			age := now.Sub(t.CreationTimestamp.Time)
			if age < gracePeriod {
				klog.V(3).InfoS("template-reconciler: unreferenced but within grace period", "namespace", ns, "name", t.Name, "age", age.Round(time.Second))
				continue
			}
			klog.V(2).InfoS("template-reconciler: deleting orphaned template", "namespace", ns, "name", t.Name, "age", age.Round(time.Second))
			if err := resourceClient.ResourceClaimTemplates(ns).Delete(ctx, t.Name, metav1.DeleteOptions{}); err != nil {
				klog.ErrorS(err, "template-reconciler: delete failed", "namespace", ns, "name", t.Name)
			} else {
				orphaned++
			}
		}
	}

	if orphaned > 0 {
		metrics.WebhookReconcilerTemplatesCleanedTotal.Add(float64(orphaned))
		klog.InfoS("template-reconciler: cleaned up orphaned templates", "count", orphaned)
	}
}
