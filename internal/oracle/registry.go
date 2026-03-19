package oracle

import (
	"context"
	"fmt"
	"strings"

	"aichain/internal/protocol"
)

type Result struct {
	Value      float64
	ObservedAt int64
	RawHash    string
}

type Adapter interface {
	Name() string
	Resolve(context.Context, protocol.Task) (Result, error)
}

type Registry struct {
	adapters map[string]Adapter
}

func NewRegistry(adapters ...Adapter) *Registry {
	registry := &Registry{adapters: make(map[string]Adapter, len(adapters))}
	for _, adapter := range adapters {
		if adapter == nil {
			continue
		}
		registry.adapters[strings.TrimSpace(adapter.Name())] = adapter
	}
	return registry
}

func Default(httpTimeoutSeconds int, allowPrivateTargets bool) *Registry {
	return NewRegistry(NewHTTPJSONAdapter(httpTimeoutSeconds, allowPrivateTargets))
}

func (r *Registry) Resolve(ctx context.Context, task protocol.Task) (Result, error) {
	source := strings.TrimSpace(task.Input.OracleSource)
	if source == "" {
		return Result{}, fmt.Errorf("oracle source is required")
	}
	adapter, ok := r.adapters[source]
	if !ok {
		return Result{}, fmt.Errorf("unsupported oracle adapter %q", source)
	}
	return adapter.Resolve(ctx, task)
}
