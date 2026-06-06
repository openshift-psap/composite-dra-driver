package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	resourceclient "k8s.io/client-go/kubernetes/typed/resource/v1"
	"k8s.io/klog/v2"
)

const (
	// Annotation to request composite device pairs.
	// Value: "<count>" e.g. "4" for 4 GPU-NIC pairs.
	PairRequestAnnotation = "composite.dra/gpu-nic-pairs"

	// Annotation set after mutation to prevent re-processing.
	MutatedAnnotation = "composite.dra/mutated"
)

// MutatorConfig holds the webhook configuration.
type MutatorConfig struct {
	DeviceClassName string
}

// Mutator generates ResourceClaimTemplates for composite devices with NUMA constraints.
type Mutator struct {
	cfg    MutatorConfig
	client resourceclient.ResourceV1Interface
}

func NewMutator(cfg MutatorConfig, client resourceclient.ResourceV1Interface) *Mutator {
	return &Mutator{cfg: cfg, client: client}
}

// Mutate processes a pod and returns JSON patch operations.
// Returns nil if no mutation is needed.
func (m *Mutator) Mutate(ctx context.Context, pod *corev1.Pod, namespace string) ([]PatchOp, error) {
	if pod.Annotations[MutatedAnnotation] == "true" {
		return nil, nil
	}

	pairCount, err := parsePairRequest(pod)
	if err != nil {
		return nil, err
	}
	if pairCount == 0 {
		return nil, nil
	}

	claimSpec := BuildClaimSpec(m.cfg.DeviceClassName, pairCount)

	templateName := fmt.Sprintf("%s-composite-pairs", pod.GenerateName)
	if templateName == "-composite-pairs" {
		templateName = fmt.Sprintf("%s-composite-pairs", pod.Name)
	}
	if len(templateName) > 63 {
		templateName = templateName[:63]
	}

	template := &resourceapi.ResourceClaimTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      templateName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "composite-dra-webhook",
			},
		},
		Spec: resourceapi.ResourceClaimTemplateSpec{
			Spec: claimSpec,
		},
	}

	_, err = m.client.ResourceClaimTemplates(namespace).Create(ctx, template, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create claim template: %w", err)
	}

	klog.Infof("webhook: created ResourceClaimTemplate %s/%s (%d pairs)",
		namespace, templateName, pairCount)

	patches := buildPatches(pod, templateName, pairCount)
	return patches, nil
}

func parsePairRequest(pod *corev1.Pod) (int, error) {
	val, ok := pod.Annotations[PairRequestAnnotation]
	if !ok {
		return 0, nil
	}

	pairCount, err := strconv.Atoi(val)
	if err != nil || pairCount <= 0 {
		return 0, fmt.Errorf("invalid pair count in annotation %q: %s", PairRequestAnnotation, val)
	}

	return pairCount, nil
}

// PatchOp is a JSON Patch operation.
type PatchOp struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func buildPatches(pod *corev1.Pod, templateName string, pairCount int) []PatchOp {
	var patches []PatchOp

	// Remove annotation
	patches = append(patches, PatchOp{
		Op:   "remove",
		Path: "/metadata/annotations/composite.dra~1gpu-nic-pairs",
	})

	// Add mutated annotation
	patches = append(patches, PatchOp{
		Op:    "add",
		Path:  "/metadata/annotations/composite.dra~1mutated",
		Value: "true",
	})

	// Add resourceClaims to pod spec
	claimRef := corev1.PodResourceClaim{
		Name:                      "composite-pairs",
		ResourceClaimTemplateName: &templateName,
	}

	if pod.Spec.ResourceClaims == nil {
		patches = append(patches, PatchOp{
			Op:    "add",
			Path:  "/spec/resourceClaims",
			Value: []corev1.PodResourceClaim{claimRef},
		})
	} else {
		patches = append(patches, PatchOp{
			Op:    "add",
			Path:  "/spec/resourceClaims/-",
			Value: claimRef,
		})
	}

	// Add claims references to first container
	if len(pod.Spec.Containers) > 0 {
		var claimRefs []corev1.ResourceClaim
		for i := 0; i < pairCount; i++ {
			claimRefs = append(claimRefs, corev1.ResourceClaim{
				Name:    "composite-pairs",
				Request: fmt.Sprintf("pair-%d", i),
			})
		}

		if pod.Spec.Containers[0].Resources.Claims == nil {
			patches = append(patches, PatchOp{
				Op:    "add",
				Path:  "/spec/containers/0/resources/claims",
				Value: claimRefs,
			})
		} else {
			for _, ref := range claimRefs {
				patches = append(patches, PatchOp{
					Op:    "add",
					Path:  "/spec/containers/0/resources/claims/-",
					Value: ref,
				})
			}
		}
	}

	return patches
}

// PatchesToJSON serializes patches to JSON.
func PatchesToJSON(patches []PatchOp) ([]byte, error) {
	return json.Marshal(patches)
}
