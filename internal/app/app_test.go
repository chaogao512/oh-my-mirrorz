package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/adapters"
	"github.com/chaogao512/oh-my-mirrorz/internal/adapters/conda"
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
func (f fakeAdapter) ProbeTargets(_ model.Environment, selection model.Selection) ([]model.ProbeTarget, error) {
	return []model.ProbeTarget{{Capability: "test", URL: selection.Endpoint, Rankable: true}}, nil
}
func (f fakeAdapter) Verify(context.Context, model.Environment, model.Selection) model.Verification {
	return model.Verification{AdapterID: f.id, OK: true}
}

type failProber struct{}

func (failProber) Probe(context.Context, string) (resolver.ProbeResult, error) {
	return resolver.ProbeResult{}, errors.New("offline")
}

type endpointProber func(string) (resolver.ProbeResult, error)

func (p endpointProber) Probe(_ context.Context, endpoint string) (resolver.ProbeResult, error) {
	return p(endpoint)
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

func TestPreferFallsBackToAutoWhenNamedMirrorFailsPreflight(t *testing.T) {
	a := App{
		Registry: adapters.NewRegistry(fakeAdapter{id: "pypi", detection: model.Detection{AdapterID: "pypi", Status: model.StatusDetected, Scope: model.ScopeUser}}),
		Resolver: resolver.New(),
		Prober: endpointProber(func(endpoint string) (resolver.ProbeResult, error) {
			if strings.Contains(endpoint, "tuna") {
				return resolver.ProbeResult{}, errors.New("offline")
			}
			return resolver.ProbeResult{Endpoint: endpoint, FinalURL: "https://mirror.example/", Status: 200}, nil
		}),
	}
	plan, err := a.Prepare(context.Background(), SwitchOptions{Strategy: model.StrategyPrefer, Mirror: "tuna"})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Selections["pypi"]; got.Mirror != "auto" || got.Strategy != model.StrategyPrefer || !strings.Contains(got.Reason, "failed preflight") {
		t.Fatalf("unexpected fallback selection: %#v", got)
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

func TestCondaSwitchAndRestoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".condarc")
	before := []byte("# original\nchannels:\n  - defaults\n  - conda-forge\nchannel_priority: strict\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	a := App{
		Env:      model.Environment{Home: root, GOOS: "darwin", GOARCH: "arm64"},
		Registry: adapters.NewRegistry(conda.New()),
		Resolver: resolver.New(),
		Prober: endpointProber(func(endpoint string) (resolver.ProbeResult, error) {
			return resolver.ProbeResult{Endpoint: endpoint, FinalURL: endpoint, Status: 200}, nil
		}),
		Store: state.New(filepath.Join(root, "state")),
	}
	result := a.Switch(context.Background(), SwitchOptions{Strategy: model.StrategyAuto, Only: []string{"conda"}, Confirm: func(Plan) bool { return true }})
	if result.Err != nil || result.Manifest == nil {
		t.Fatalf("switch result=%#v", result)
	}
	after, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(after), "https://mirrors.cernet.edu.cn/anaconda") || !strings.Contains(string(after), "channel_priority: strict") {
		t.Fatalf("after=%s err=%v", after, err)
	}
	restored := a.Restore(result.Manifest.ID)
	if restored.Err != nil {
		t.Fatal(restored.Err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(before) {
		t.Fatalf("restored=%q err=%v", got, err)
	}
}
