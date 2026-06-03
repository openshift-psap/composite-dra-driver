package synthesizer

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/klog/v2"
)

// CELFilter evaluates CEL expressions against device attributes.
// Compiled programs are cached for reuse.
type CELFilter struct {
	mu    sync.Mutex
	cache map[string]cel.Program
	env   *cel.Env
}

func NewCELFilter() (*CELFilter, error) {
	env, err := cel.NewEnv(
		cel.Variable("device", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return nil, fmt.Errorf("create CEL env: %w", err)
	}
	return &CELFilter{
		cache: make(map[string]cel.Program),
		env:   env,
	}, nil
}

// Match evaluates a CEL expression against device attributes.
// The expression receives a "device" variable with nested attribute access:
//
//	device.attributes["dra.net"].rdma == true
//	device.attributes["dra.net"].ipv4.startsWith("10.0.")
//
// We flatten this to: device.attributes is a map[string]any where keys are
// domain/attrName and values are the Go primitives.
func (f *CELFilter) Match(expression string, attrs map[string]resourceapi.DeviceAttribute) bool {
	prog, err := f.getProgram(expression)
	if err != nil {
		klog.V(2).Infof("cel: compile %q: %v", expression, err)
		return false
	}

	deviceMap := buildDeviceMap(attrs)
	out, _, err := prog.Eval(map[string]interface{}{
		"device": deviceMap,
	})
	if err != nil {
		klog.V(4).Infof("cel: eval %q: %v", expression, err)
		return false
	}

	if out.Type() == types.BoolType {
		return out.Value().(bool)
	}
	return false
}

func (f *CELFilter) getProgram(expression string) (cel.Program, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if prog, ok := f.cache[expression]; ok {
		return prog, nil
	}

	ast, issues := f.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}

	prog, err := f.env.Program(ast)
	if err != nil {
		return nil, err
	}

	f.cache[expression] = prog
	return prog, nil
}

// buildDeviceMap builds the nested map structure for CEL evaluation.
// Converts: {"dra.net/rdma": bool(true), "dra.net/ipv4": string("10.0.0.5")}
// To: {"attributes": {"dra.net": {"rdma": true, "ipv4": "10.0.0.5"}}}
func buildDeviceMap(attrs map[string]resourceapi.DeviceAttribute) map[string]interface{} {
	attrsByDomain := make(map[string]map[string]interface{})

	for key, attr := range attrs {
		domain, name := splitAttrKey(key)
		if attrsByDomain[domain] == nil {
			attrsByDomain[domain] = make(map[string]interface{})
		}
		attrsByDomain[domain][name] = attrToValue(attr)
	}

	// Convert inner maps to ref.Val-compatible types
	domainMap := make(map[string]interface{}, len(attrsByDomain))
	for domain, inner := range attrsByDomain {
		domainMap[domain] = inner
	}

	return map[string]interface{}{
		"attributes": domainMap,
	}
}

func splitAttrKey(key string) (domain, name string) {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '/' {
			return key[:i], key[i+1:]
		}
	}
	return "", key
}

func attrToValue(attr resourceapi.DeviceAttribute) interface{} {
	if attr.BoolValue != nil {
		return *attr.BoolValue
	}
	if attr.StringValue != nil {
		return *attr.StringValue
	}
	if attr.IntValue != nil {
		return *attr.IntValue
	}
	return nil
}

// Ensure ref.Val interface is not needed — cel-go handles Go native types.
var _ ref.Val = nil // compile check that cel-go is importable
