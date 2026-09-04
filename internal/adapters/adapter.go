package adapters

import (
	"context"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

type Adapter interface {
	ID() string
	Detect(context.Context, model.Environment) model.Detection
	Inspect(context.Context, model.Environment) ([]byte, error)
	Plan(context.Context, model.Environment, model.Selection) ([]model.Change, error)
	ProbeTargets(model.Environment, model.Selection) ([]model.ProbeTarget, error)
	Verify(context.Context, model.Environment, model.Selection) model.Verification
}

type Registry struct {
	items map[string]Adapter
	order []string
}

func NewRegistry(items ...Adapter) *Registry {
	r := &Registry{items: make(map[string]Adapter, len(items))}
	for _, item := range items {
		id := item.ID()
		if _, exists := r.items[id]; !exists {
			r.order = append(r.order, id)
		}
		r.items[id] = item
	}
	return r
}

func (r *Registry) Get(id string) (Adapter, bool) {
	a, ok := r.items[id]
	return a, ok
}

func (r *Registry) All() []Adapter {
	result := make([]Adapter, 0, len(r.order))
	for _, id := range r.order {
		result = append(result, r.items[id])
	}
	return result
}

func (r *Registry) IDs() []string {
	return append([]string(nil), r.order...)
}
