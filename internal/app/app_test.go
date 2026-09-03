package app

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/adapters"
	"github.com/chaogao512/oh-my-mirrorz/internal/model"
	"github.com/chaogao512/oh-my-mirrorz/internal/resolver"
	"github.com/chaogao512/oh-my-mirrorz/internal/state"
)

type fakeAdapter struct {
	id        string
	detection model.Detection
	changes   []model.Change
}

func (f fakeAdapter) ID() string                                                 { return f.id }
func (f fakeAdapter) Detect(context.Context, model.Environment) model.Detection  { return f.detection }
func (f fakeAdapter) Inspect(context.Context, model.Environment) ([]byte, error) { return nil, nil }
func (f fakeAdapter) Plan(context.Context, model.Environment, model.Selection) ([]model.Change, error) {
	return f.changes, nil
}
func (f fakeAdapter) Verify(context.Context, model.Environment, model.Selection) model.Verification {
	return model.Verification{AdapterID: f.id, OK: true}
}

type failProber struct{}

func (failProber) Probe(context.Context, string) (resolver.ProbeResult, error) {
	return resolver.ProbeResult{}, errors.New("offline")
}

func TestPrepareRejectsOnlyAndExclude(t *testing.T) {
	a := App{Registry: adapters.NewRegistry(fakeAdapter{id: "pypi"}), Resolver: resolver.New()}
	_, err := a.Prepare(context.Background(), SwitchOptions{Strategy: model.StrategyAuto, Only: []string{"pypi"}, Exclude: []string{"pypi"}})
	if err == nil {
		t.Fatal("expected conflict")
	}
}

func TestPrepareSkipsSystemWithoutOptIn(t *testing.T) {
	a := App{
		Registry: adapters.NewRegistry(fakeAdapter{id: "apt", detection: model.Detection{AdapterID: "apt", Status: model.StatusDetected, Scope: model.ScopeSystem}}),
		Resolver: resolver.New(),
	}
	plan, err := a.Prepare(context.Background(), SwitchOptions{Strategy: model.StrategyAuto})
	if err != nil || len(plan.Adapters) != 0 {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
}

func TestDryRunDoesNotCreateState(t *testing.T) {
	root := t.TempDir()
	change := model.Change{AdapterID: "pypi", Path: filepath.Join(root, "pip.conf"), Scope: model.ScopeUser, After: []byte("x"), Mode: 0o600}
	a := App{
		Registry: adapters.NewRegistry(fakeAdapter{id: "pypi", detection: model.Detection{AdapterID: "pypi", Status: model.StatusDetected, Scope: model.ScopeUser}, changes: []model.Change{change}}),
		Resolver: resolver.New(), Store: state.New(filepath.Join(root, "state")),
	}
	result := a.Switch(context.Background(), SwitchOptions{Strategy: model.StrategyAuto, DryRun: true})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if history, err := a.Store.List(); err != nil || len(history) != 0 {
		t.Fatalf("history = %#v, %v", history, err)
	}
}

func TestProbeFailureStopsPlan(t *testing.T) {
	a := App{
		Registry: adapters.NewRegistry(fakeAdapter{id: "pypi", detection: model.Detection{AdapterID: "pypi", Status: model.StatusDetected, Scope: model.ScopeUser}}),
		Resolver: resolver.New(), Prober: failProber{},
	}
	if _, err := a.Prepare(context.Background(), SwitchOptions{Strategy: model.StrategyAuto}); err == nil {
		t.Fatal("expected probe failure")
	}
}

func TestSwitchBlocksWhenTransactionNeedsRecovery(t *testing.T) {
	root := t.TempDir()
	store := state.New(filepath.Join(root, "state"))
	if _, err := store.Create(nil); err != nil {
		t.Fatal(err)
	}
	a := App{
		Registry: adapters.NewRegistry(fakeAdapter{id: "pypi", detection: model.Detection{AdapterID: "pypi", Status: model.StatusDetected, Scope: model.ScopeUser}}),
		Resolver: resolver.New(), Store: store,
	}
	result := a.Switch(context.Background(), SwitchOptions{Strategy: model.StrategyAuto})
	if result.Err == nil {
		t.Fatal("expected unfinished transaction to block switch")
	}
}

func TestPrintPlanIncludesScopeAndFieldLevelTarget(t *testing.T) {
	var output bytes.Buffer
	a := App{Out: &output}
	a.PrintPlan(Plan{
		Selections: map[string]model.Selection{"npm": {Mirror: "auto", Endpoint: "https://registry.example/"}},
		Changes:    []model.Change{{AdapterID: "npm", Path: "/tmp/.npmrc", Scope: model.ScopeUser, Before: []byte("old"), After: []byte("new"), Existed: true}},
	})
	text := output.String()
	for _, want := range []string{"mirror=auto", "registry -> https://registry.example/", "scope=user", "/tmp/.npmrc"} {
		if !strings.Contains(text, want) {
			t.Fatalf("plan missing %q:\n%s", want, text)
		}
	}
}
