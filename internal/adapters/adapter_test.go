package adapters

import (
	"context"
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

type fakeAdapter string

func (f fakeAdapter) ID() string { return string(f) }
func (f fakeAdapter) Detect(context.Context, model.Environment) model.Detection {
	return model.Detection{AdapterID: string(f), Status: model.StatusDetected}
}
func (f fakeAdapter) Inspect(context.Context, model.Environment) ([]byte, error) { return nil, nil }
func (f fakeAdapter) Plan(context.Context, model.Environment, model.Selection) ([]model.Change, error) {
	return nil, nil
}
func (f fakeAdapter) Verify(context.Context, model.Environment, model.Selection) model.Verification {
	return model.Verification{AdapterID: string(f), OK: true}
}

func TestRegistryKeepsStableOrderAndReplacesDuplicate(t *testing.T) {
	r := NewRegistry(fakeAdapter("pip"), fakeAdapter("npm"), fakeAdapter("pip"))
	if got := r.IDs(); len(got) != 2 || got[0] != "pip" || got[1] != "npm" {
		t.Fatalf("unexpected IDs: %#v", got)
	}
	if _, ok := r.Get("pip"); !ok {
		t.Fatal("expected pip adapter")
	}
}
