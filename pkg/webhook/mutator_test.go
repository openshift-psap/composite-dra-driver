package webhook

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fake "k8s.io/client-go/kubernetes/typed/resource/v1/fake"
	k8stesting "k8s.io/client-go/testing"
)

func newTestMutator(mappings map[string]string) *Mutator {
	fakeClient := &fake.FakeResourceV1{Fake: &k8stesting.Fake{}}
	fakeClient.Fake.AddReactor("create", "resourceclaimtemplates", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, action.(k8stesting.CreateAction).GetObject(), nil
	})
	return NewMutator(MutatorConfig{
		ResourceMappings: mappings,
	}, fakeClient)
}

func singleMapping() map[string]string {
	return map[string]string{"composite.dra/gpu-nic-pair": "composite-gpu-nic-pair"}
}

func TestMutate_SkipsPodWithoutSyntheticResource(t *testing.T) {
	m := newTestMutator(singleMapping())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "normal-pod"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
		},
	}

	patches, err := m.Mutate(context.Background(), pod, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patches != nil {
		t.Fatalf("expected nil patches for pod without synthetic resource, got %d patches", len(patches))
	}
}

func TestMutate_SkipsPodWithDifferentResource(t *testing.T) {
	m := newTestMutator(singleMapping())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "other-resource-pod"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	patches, err := m.Mutate(context.Background(), pod, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patches != nil {
		t.Fatalf("expected nil patches for pod with different resource, got %d patches", len(patches))
	}
}

func TestMutate_MutatesPodWithSyntheticResource(t *testing.T) {
	m := newTestMutator(singleMapping())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "test-pod-"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("composite.dra/gpu-nic-pair"): resource.MustParse("2"),
						},
					},
				},
			},
		},
	}

	patches, err := m.Mutate(context.Background(), pod, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patches == nil {
		t.Fatal("expected patches for pod with synthetic resource, got nil")
	}
	if len(patches) == 0 {
		t.Fatal("expected non-empty patches")
	}
}

func TestMutate_SkipsAlreadyMutatedPod(t *testing.T) {
	m := newTestMutator(singleMapping())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "already-mutated",
			Annotations: map[string]string{MutatedAnnotation: "true"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("composite.dra/gpu-nic-pair"): resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	patches, err := m.Mutate(context.Background(), pod, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patches != nil {
		t.Fatalf("expected nil patches for already-mutated pod, got %d patches", len(patches))
	}
}

func TestMutate_SkipsZeroCountResource(t *testing.T) {
	m := newTestMutator(singleMapping())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "zero-count"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("composite.dra/gpu-nic-pair"): resource.MustParse("0"),
						},
					},
				},
			},
		},
	}

	patches, err := m.Mutate(context.Background(), pod, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patches != nil {
		t.Fatalf("expected nil patches for zero-count resource, got %d patches", len(patches))
	}
}

func TestMutate_MultipleResourceTypes(t *testing.T) {
	m := newTestMutator(map[string]string{
		"composite.dra/gpu-nic-pair":  "composite-gpu-nic-pair",
		"composite.dra/gpu-fpga-pair": "composite-gpu-fpga-pair",
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "multi-comp"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("composite.dra/gpu-nic-pair"):  resource.MustParse("2"),
							corev1.ResourceName("composite.dra/gpu-fpga-pair"): resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	patches, err := m.Mutate(context.Background(), pod, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patches == nil {
		t.Fatal("expected patches for multi-resource pod")
	}

	// Count remove ops (should remove both synthetic resources)
	removeCount := 0
	addClaimsCount := 0
	for _, p := range patches {
		if p.Op == "remove" {
			removeCount++
		}
		if p.Op == "add" && p.Path == "/spec/resourceClaims" || p.Path == "/spec/resourceClaims/-" {
			addClaimsCount++
		}
	}
	if removeCount != 2 {
		t.Errorf("expected 2 remove ops (one per resource), got %d", removeCount)
	}
}

func TestMutate_OnlyMatchingResources(t *testing.T) {
	m := newTestMutator(map[string]string{
		"composite.dra/gpu-nic-pair": "composite-gpu-nic-pair",
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "partial-match"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("composite.dra/gpu-nic-pair"): resource.MustParse("1"),
							corev1.ResourceName("nvidia.com/gpu"):             resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	patches, err := m.Mutate(context.Background(), pod, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patches == nil {
		t.Fatal("expected patches")
	}

	// nvidia.com/gpu should NOT be removed
	for _, p := range patches {
		if p.Op == "remove" && p.Path == "/spec/containers/0/resources/requests/nvidia.com~1gpu" {
			t.Error("should not remove non-composite resources")
		}
	}
}
