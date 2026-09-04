package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/chaogao512/oh-my-mirrorz/internal/adapters"
	"github.com/chaogao512/oh-my-mirrorz/internal/model"
	"github.com/chaogao512/oh-my-mirrorz/internal/resolver"
	"github.com/chaogao512/oh-my-mirrorz/internal/safeurl"
	"github.com/chaogao512/oh-my-mirrorz/internal/state"
	"github.com/chaogao512/oh-my-mirrorz/internal/transaction"
)

type App struct {
	Env      model.Environment
	Registry *adapters.Registry
	Resolver *resolver.Resolver
	Store    *state.Store
	Prober   resolver.Prober
	Writer   transaction.Writer
	Out      io.Writer
}

type SwitchOptions struct {
	Strategy model.Strategy
	Mirror   string
	Only     []string
	Exclude  []string
	DryRun   bool
	Confirm  func(Plan) bool
}

type Plan struct {
	Detections []model.Detection
	Selections map[string]model.Selection
	Probes     map[string][]resolver.ProbeResult
	Changes    []model.Change
	Adapters   []adapters.Adapter
}

func (a *App) output() io.Writer {
	if a.Out == nil {
		return io.Discard
	}
	return a.Out
}

func (a *App) Scan(ctx context.Context) ([]model.Detection, error) {
	if a.Registry == nil {
		return nil, errors.New("adapter registry is nil")
	}
	result := make([]model.Detection, 0, len(a.Registry.All()))
	for _, adapter := range a.Registry.All() {
		result = append(result, adapter.Detect(ctx, a.Env))
	}
	return result, nil
}

func (a *App) Prepare(ctx context.Context, options SwitchOptions) (Plan, error) {
	if len(options.Only) > 0 && len(options.Exclude) > 0 {
		return Plan{}, errors.New("--only and --exclude cannot be used together")
	}
	if a.Resolver == nil {
		return Plan{}, errors.New("mirror resolver is nil")
	}
	detections, err := a.Scan(ctx)
	if err != nil {
		return Plan{}, err
	}
	selected, err := a.filterAdapters(options.Only, options.Exclude)
	if err != nil {
		return Plan{}, err
	}
	detectionByID := make(map[string]model.Detection, len(detections))
	for _, detection := range detections {
		detectionByID[detection.AdapterID] = detection
	}
	plan := Plan{Detections: detections, Selections: map[string]model.Selection{}, Probes: map[string][]resolver.ProbeResult{}}
	for _, adapter := range selected {
		detection := detectionByID[adapter.ID()]
		switch detection.Status {
		case model.StatusNotInstalled, model.StatusUnsupported:
			continue
		case model.StatusInvalidConfig:
			return Plan{}, fmt.Errorf("%s has invalid configuration: %s", adapter.ID(), detection.Reason)
		case model.StatusDetected:
		default:
			continue
		}
		if detection.Scope == model.ScopeSystem && !a.Env.IncludeSystem {
			continue
		}
		selection, err := a.Resolver.Resolve(adapter.ID(), options.Strategy, options.Mirror)
		if err != nil {
			return Plan{}, fmt.Errorf("resolve %s: %w", adapter.ID(), err)
		}
		if a.Prober != nil {
			results, probeErr := probeSelection(ctx, a.Prober, adapter, a.Env, selection)
			if probeErr != nil && options.Strategy == model.StrategyPrefer && selection.Mirror != "auto" {
				preferred := selection.Mirror
				selection, err = a.Resolver.Resolve(adapter.ID(), model.StrategyAuto, "")
				if err == nil {
					selection.Strategy = model.StrategyPrefer
					selection.Reason = fmt.Sprintf("preferred mirror %q failed preflight; %s", preferred, selection.Reason)
					results, probeErr = probeSelection(ctx, a.Prober, adapter, a.Env, selection)
				}
			}
			if probeErr != nil {
				return Plan{}, fmt.Errorf("probe %s: %w", adapter.ID(), probeErr)
			}
			plan.Probes[adapter.ID()] = results
		}
		changes, err := adapter.Plan(ctx, a.Env, selection)
		if err != nil {
			return Plan{}, fmt.Errorf("plan %s: %w", adapter.ID(), err)
		}
		for _, change := range changes {
			if change.Changed() {
				plan.Changes = append(plan.Changes, change)
			}
		}
		plan.Selections[adapter.ID()] = selection
		plan.Adapters = append(plan.Adapters, adapter)
	}
	return plan, nil
}

func (a *App) Switch(ctx context.Context, options SwitchOptions) transaction.Result {
	if !options.DryRun {
		if err := a.ensureNoUnfinishedTransaction(); err != nil {
			return transaction.Result{Err: err}
		}
	}
	plan, err := a.Prepare(ctx, options)
	if err != nil {
		return transaction.Result{Err: err}
	}
	a.PrintPlan(plan)
	if options.DryRun || len(plan.Changes) == 0 {
		return transaction.Result{}
	}
	if options.Confirm != nil && !options.Confirm(plan) {
		return transaction.Result{Err: errors.New("operation cancelled")}
	}
	engine := transaction.Engine{Store: a.Store, Writer: a.Writer}
	return engine.Run(ctx, plan.Changes, func(ctx context.Context) error {
		for _, adapter := range plan.Adapters {
			verification := adapter.Verify(ctx, a.Env, plan.Selections[adapter.ID()])
			if !verification.OK {
				return fmt.Errorf("%s verification failed: %s", adapter.ID(), verification.Detail)
			}
		}
		return nil
	})
}

func (a *App) ensureNoUnfinishedTransaction() error {
	if a.Store == nil {
		return errors.New("transaction store is nil")
	}
	history, err := a.Store.List()
	if err != nil {
		return err
	}
	for _, manifest := range history {
		switch manifest.Status {
		case state.StatusPrepared, state.StatusSnapshotted, state.StatusApplying, state.StatusVerifying, state.StatusFailed, state.StatusRollingBack, state.StatusDegraded:
			return fmt.Errorf("transaction %s requires recovery (%s)", manifest.ID, manifest.Status)
		}
	}
	return nil
}

func (a *App) PrintPlan(plan Plan) {
	if len(plan.Changes) == 0 {
		fmt.Fprintln(a.output(), "No changes are required.")
		return
	}
	fmt.Fprintf(a.output(), "Plan: %d file change(s)\n", len(plan.Changes))
	ids := make([]string, 0, len(plan.Selections))
	for id := range plan.Selections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		selection := plan.Selections[id]
		fmt.Fprintf(a.output(), "  target %-10s mirror=%s  %s\n", id, selection.Mirror, selectionFields(id, selection, a.Env.IncludeSecurity))
		for _, probe := range plan.Probes[id] {
			fmt.Fprintf(a.output(), "  route  %-10s %s  %s\n", id, safeurl.Redact(probe.FinalURL), probe.Latency.Round(time.Millisecond))
		}
	}
	if a.Env.IncludeSecurity {
		if _, ok := plan.Selections["apt"]; ok {
			fmt.Fprintln(a.output(), "  warning: security repositories are included; mirror synchronization may delay security updates")
		}
	}
	for _, change := range plan.Changes {
		before := state.Sum(change.Before)[:12]
		after := state.Sum(change.After)[:12]
		fmt.Fprintf(a.output(), "  file   %-10s scope=%-6s %s [%s -> %s]\n", change.AdapterID, change.Scope, change.Path, before, after)
	}
}

func selectionFields(adapterID string, selection model.Selection, includeSecurity bool) string {
	endpoint := func(keys ...string) string {
		for _, key := range keys {
			if value := selection.Endpoints[key]; value != "" {
				return safeurl.Redact(value)
			}
		}
		return safeurl.Redact(selection.Endpoint)
	}
	switch adapterID {
	case "pypi":
		return "index-url -> " + endpoint("pypi", "pip", "uv")
	case "npm":
		return "registry -> " + endpoint("npm")
	case "cargo":
		return "source.crates-io -> " + endpoint("cargo")
	case "homebrew":
		return fmt.Sprintf("API -> %s; bottles -> %s; pip -> %s", endpoint("api"), endpoint("bottles"), endpoint("pypi"))
	case "apt":
		text := fmt.Sprintf("URIs -> %s; ports -> %s", endpoint("ubuntu", "debian"), endpoint("ubuntu-ports"))
		if includeSecurity {
			text += "; security -> " + endpoint("debian-security", "ubuntu")
		}
		return text
	case "conda":
		return "public channels -> " + endpoint("anaconda")
	default:
		return "endpoint -> " + endpoint()
	}
}

func (a *App) History() ([]state.Manifest, error) {
	if a.Store == nil {
		return nil, errors.New("transaction store is nil")
	}
	return a.Store.List()
}

func (a *App) Restore(id string) transaction.Result {
	if a.Store == nil {
		return transaction.Result{Err: errors.New("transaction store is nil")}
	}
	if id == "" {
		history, err := a.Store.List()
		if err != nil {
			return transaction.Result{Err: err}
		}
		for _, manifest := range history {
			if manifest.Status == state.StatusCommitted || manifest.Status == state.StatusDegraded || manifest.Status == state.StatusFailed {
				id = manifest.ID
				break
			}
		}
		if id == "" {
			return transaction.Result{Err: errors.New("no restorable transaction found")}
		}
	}
	engine := transaction.Engine{Store: a.Store, Writer: a.Writer}
	return engine.Restore(id)
}

func (a *App) Doctor(ctx context.Context) error {
	detections, err := a.Scan(ctx)
	if err != nil {
		return err
	}
	for _, detection := range detections {
		if detection.Status == model.StatusInvalidConfig {
			return fmt.Errorf("%s configuration is invalid: %s", detection.AdapterID, detection.Reason)
		}
	}
	if a.Store == nil {
		return errors.New("transaction store is nil")
	}
	return a.ensureNoUnfinishedTransaction()
}

func (a *App) filterAdapters(only, exclude []string) ([]adapters.Adapter, error) {
	onlySet, err := a.validateIDs(only)
	if err != nil {
		return nil, err
	}
	excludeSet, err := a.validateIDs(exclude)
	if err != nil {
		return nil, err
	}
	var result []adapters.Adapter
	for _, adapter := range a.Registry.All() {
		if len(onlySet) > 0 && !onlySet[adapter.ID()] {
			continue
		}
		if excludeSet[adapter.ID()] {
			continue
		}
		result = append(result, adapter)
	}
	return result, nil
}

func (a *App) validateIDs(ids []string) (map[string]bool, error) {
	result := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := a.Registry.Get(id); !ok {
			return nil, fmt.Errorf("unknown adapter %q", id)
		}
		result[id] = true
	}
	return result, nil
}

func probeSelection(ctx context.Context, prober resolver.Prober, adapter adapters.Adapter, env model.Environment, selection model.Selection) ([]resolver.ProbeResult, error) {
	targets, err := adapter.ProbeTargets(env, selection)
	if err != nil {
		return nil, err
	}
	results := make([]resolver.ProbeResult, 0, len(targets))
	for _, target := range targets {
		result, err := prober.Probe(ctx, target.URL)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", target.Capability, err)
		}
		results = append(results, result)
	}
	return results, nil
}
