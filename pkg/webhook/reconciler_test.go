// Copyright 2026 Red Hat, LLC. and/or its affiliates
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekube "k8s.io/client-go/kubernetes/fake"
)

func TestReconcileTemplates(t *testing.T) {
	now := time.Now()
	old := metav1.NewTime(now.Add(-10 * time.Minute))
	recent := metav1.NewTime(now.Add(-30 * time.Second))
	templateName := func(name string) *string { return &name }

	tests := []struct {
		name            string
		templates       []resourceapi.ResourceClaimTemplate
		pods            []corev1.Pod
		gracePeriod     time.Duration
		wantDeleted     int
		wantSurvivors   []string
	}{
		{
			name:        "no templates",
			gracePeriod: 2 * time.Minute,
			wantDeleted: 0,
		},
		{
			name: "orphaned template deleted",
			templates: []resourceapi.ResourceClaimTemplate{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gone-pod-composite-gpu-nic", Namespace: "default",
						Labels:            map[string]string{ManagedByLabel: ManagedByValue},
						CreationTimestamp: old,
					},
				},
			},
			gracePeriod: 2 * time.Minute,
			wantDeleted: 1,
		},
		{
			name: "referenced template kept",
			templates: []resourceapi.ResourceClaimTemplate{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-pod-composite-gpu-nic", Namespace: "default",
						Labels:            map[string]string{ManagedByLabel: ManagedByValue},
						CreationTimestamp: old,
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "default"},
					Spec: corev1.PodSpec{
						ResourceClaims: []corev1.PodResourceClaim{
							{Name: "composite-gpu-nic", ResourceClaimTemplateName: templateName("my-pod-composite-gpu-nic")},
						},
						Containers: []corev1.Container{{Name: "app"}},
					},
				},
			},
			gracePeriod:   2 * time.Minute,
			wantDeleted:   0,
			wantSurvivors: []string{"my-pod-composite-gpu-nic"},
		},
		{
			name: "grace period respected",
			templates: []resourceapi.ResourceClaimTemplate{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "new-pod-composite-gpu-nic", Namespace: "default",
						Labels:            map[string]string{ManagedByLabel: ManagedByValue},
						CreationTimestamp: recent,
					},
				},
			},
			gracePeriod:   2 * time.Minute,
			wantDeleted:   0,
			wantSurvivors: []string{"new-pod-composite-gpu-nic"},
		},
		{
			name: "mixed: orphan deleted, referenced and young kept",
			templates: []resourceapi.ResourceClaimTemplate{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "orphan-composite-gpu-nic", Namespace: "default",
						Labels:            map[string]string{ManagedByLabel: ManagedByValue},
						CreationTimestamp: old,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "live-composite-gpu-nic", Namespace: "default",
						Labels:            map[string]string{ManagedByLabel: ManagedByValue},
						CreationTimestamp: old,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "young-composite-gpu-nic", Namespace: "default",
						Labels:            map[string]string{ManagedByLabel: ManagedByValue},
						CreationTimestamp: recent,
					},
				},
			},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "live-pod", Namespace: "default"},
					Spec: corev1.PodSpec{
						ResourceClaims: []corev1.PodResourceClaim{
							{Name: "composite-gpu-nic", ResourceClaimTemplateName: templateName("live-composite-gpu-nic")},
						},
						Containers: []corev1.Container{{Name: "app"}},
					},
				},
			},
			gracePeriod:   2 * time.Minute,
			wantDeleted:   1,
			wantSurvivors: []string{"live-composite-gpu-nic", "young-composite-gpu-nic"},
		},
		{
			name: "templates without managed-by label ignored",
			templates: []resourceapi.ResourceClaimTemplate{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "unmanaged-template", Namespace: "default",
						Labels:            map[string]string{"other": "label"},
						CreationTimestamp: old,
					},
				},
			},
			gracePeriod: 2 * time.Minute,
			wantDeleted: 0,
		},
		{
			name: "multi-namespace",
			templates: []resourceapi.ResourceClaimTemplate{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "orphan-a", Namespace: "ns-a",
						Labels:            map[string]string{ManagedByLabel: ManagedByValue},
						CreationTimestamp: old,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "orphan-b", Namespace: "ns-b",
						Labels:            map[string]string{ManagedByLabel: ManagedByValue},
						CreationTimestamp: old,
					},
				},
			},
			gracePeriod: 2 * time.Minute,
			wantDeleted: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []runtime.Object
			for i := range tt.templates {
				objects = append(objects, &tt.templates[i])
			}
			for i := range tt.pods {
				objects = append(objects, &tt.pods[i])
			}

			client := fakekube.NewSimpleClientset(objects...)

			reconcileTemplates(context.Background(), client.ResourceV1(), client.CoreV1(), tt.gracePeriod)

			remaining, err := client.ResourceV1().ResourceClaimTemplates("").List(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("list after reconcile: %v", err)
			}

			deleted := len(tt.templates) - len(remaining.Items)
			if deleted != tt.wantDeleted {
				t.Errorf("deleted %d templates, want %d", deleted, tt.wantDeleted)
			}

			survivorSet := make(map[string]bool)
			for _, r := range remaining.Items {
				survivorSet[r.Name] = true
			}
			for _, name := range tt.wantSurvivors {
				if !survivorSet[name] {
					t.Errorf("expected template %q to survive, but it was deleted", name)
				}
			}
		})
	}
}
