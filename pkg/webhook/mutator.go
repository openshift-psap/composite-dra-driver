package webhook

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	resourceclient "k8s.io/client-go/kubernetes/typed/resource/v1"
	"k8s.io/klog/v2"
)

const (
	MutatedAnnotation = "composite.dra/mutated"
)

// MutatorConfig holds the webhook configuration.
type MutatorConfig struct {
	DeviceClassName string
	ResourceName    string // e.g. "composite.dra/gpu-nic-pair"
}

// Mutator generates ResourceClaimTemplates for composite devices.
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

	pairCount, containerIndices := m.findResourceRequests(pod)
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

	_, err := m.client.ResourceClaimTemplates(namespace).Create(ctx, template, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create claim template: %w", err)
	}

	klog.Infof("webhook: created ResourceClaimTemplate %s/%s (%d pairs)",
		namespace, templateName, pairCount)

	patches := m.buildPatches(pod, templateName, pairCount, containerIndices)
	return patches, nil
}

// findResourceRequests scans all containers for the synthetic resource request.
// Returns total pair count and indices of containers that have the request.
func (m *Mutator) findResourceRequests(pod *corev1.Pod) (int, []int) {
	resName := corev1.ResourceName(m.cfg.ResourceName)
	totalCount := 0
	var indices []int

	for i := range pod.Spec.Containers {
		if qty, ok := pod.Spec.Containers[i].Resources.Requests[resName]; ok {
			count, ok := qty.AsInt64()
			if ok && count > 0 {
				totalCount += int(count)
				indices = append(indices, i)
			}
		}
	}

	return totalCount, indices
}

// PatchOp is a JSON Patch operation.
type PatchOp struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func (m *Mutator) buildPatches(pod *corev1.Pod, templateName string, pairCount int, containerIndices []int) []PatchOp {
	var patches []PatchOp
	resName := corev1.ResourceName(m.cfg.ResourceName)

	// Add mutated annotation
	if pod.Annotations == nil {
		patches = append(patches, PatchOp{
			Op:    "add",
			Path:  "/metadata/annotations",
			Value: map[string]string{MutatedAnnotation: "true"},
		})
	} else {
		patches = append(patches, PatchOp{
			Op:    "add",
			Path:  "/metadata/annotations/composite.dra~1mutated",
			Value: "true",
		})
	}

	// Remove synthetic resource from each container's requests and limits
	for _, idx := range containerIndices {
		escapedRes := jsonPatchEscape(string(resName))
		patches = append(patches, PatchOp{
			Op:   "remove",
			Path: fmt.Sprintf("/spec/containers/%d/resources/requests/%s", idx, escapedRes),
		})
		if _, ok := pod.Spec.Containers[idx].Resources.Limits[resName]; ok {
			patches = append(patches, PatchOp{
				Op:   "remove",
				Path: fmt.Sprintf("/spec/containers/%d/resources/limits/%s", idx, escapedRes),
			})
		}
	}

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

	// Add claim references to containers that had the resource request
	pairIdx := 0
	for _, containerIdx := range containerIndices {
		qty := pod.Spec.Containers[containerIdx].Resources.Requests[resName]
		count, _ := qty.AsInt64()

		var claimRefs []corev1.ResourceClaim
		for i := 0; i < int(count); i++ {
			claimRefs = append(claimRefs, corev1.ResourceClaim{
				Name:    "composite-pairs",
				Request: fmt.Sprintf("pair-%d", pairIdx),
			})
			pairIdx++
		}

		if pod.Spec.Containers[containerIdx].Resources.Claims == nil {
			patches = append(patches, PatchOp{
				Op:    "add",
				Path:  fmt.Sprintf("/spec/containers/%d/resources/claims", containerIdx),
				Value: claimRefs,
			})
		} else {
			for _, ref := range claimRefs {
				patches = append(patches, PatchOp{
					Op:    "add",
					Path:  fmt.Sprintf("/spec/containers/%d/resources/claims/-", containerIdx),
					Value: ref,
				})
			}
		}
	}

	return patches
}

// jsonPatchEscape escapes / and ~ in JSON Patch paths per RFC 6901.
func jsonPatchEscape(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '~':
			result = append(result, '~', '0')
		case '/':
			result = append(result, '~', '1')
		default:
			result = append(result, s[i])
		}
	}
	return string(result)
}

// PatchesToJSON serializes patches to JSON.
func PatchesToJSON(patches []PatchOp) ([]byte, error) {
	return json.Marshal(patches)
}

// Keep resource import used
var _ = resource.MustParse
