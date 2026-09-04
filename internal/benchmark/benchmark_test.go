package benchmark

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
	"github.com/chaogao512/oh-my-mirrorz/internal/resolver"
)

type fakeAdapter struct{}

func (fakeAdapter) ID() string { return "pypi" }
func (fakeAdapter) Detect(context.Context, model.Environment) model.Detection {
	return model.Detection{}
}
func (fakeAdapter) Inspect(context.Context, model.Environment) ([]byte, error) { return nil, nil }
func (fakeAdapter) Plan(context.Context, model.Environment, model.Selection) ([]model.Change, error) {
	return nil, nil
}
func (fakeAdapter) Verify(context.Context, model.Environment, model.Selection) model.Verification {
	return model.Verification{}
}
func (fakeAdapter) ProbeTargets(_ model.Environment, selection model.Selection) ([]model.ProbeTarget, error) {
	return []model.ProbeTarget{{Capability: "simple", URL: selection.Endpoint + "/pip/", Rankable: true}}, nil
}

type nonRankableAdapter struct{ fakeAdapter }

func (nonRankableAdapter) ProbeTargets(_ model.Environment, selection model.Selection) ([]model.ProbeTarget, error) {
	return []model.ProbeTarget{{Capability: "mirrorlist", URL: selection.Endpoint, Rankable: false}}, nil
}

type sequenceProber struct {
	results map[string][]resolver.ProbeResult
	errors  map[string]error
	index   map[string]int
}

func (p *sequenceProber) Probe(_ context.Context, endpoint string) (resolver.ProbeResult, error) {
	if err := p.errors[endpoint]; err != nil {
		return resolver.ProbeResult{}, err
	}
	i := p.index[endpoint]
	p.index[endpoint] = i + 1
	items := p.results[endpoint]
	if len(items) == 0 {
		return resolver.ProbeResult{}, errors.New("missing fake result")
	}
	return items[i%len(items)], nil
}

func TestRunUsesMedianAndMarksFastest(t *testing.T) {
	prober := &sequenceProber{index: map[string]int{}, results: map[string][]resolver.ProbeResult{
		"https://auto.example/pip/": {{Latency: 30 * time.Millisecond, Status: 200, FinalURL: "https://one.example/pip/"}, {Latency: 10 * time.Millisecond, Status: 200, FinalURL: "https://one.example/pip/"}, {Latency: 20 * time.Millisecond, Status: 200, FinalURL: "https://one.example/pip/"}},
		"https://ustc.example/pip/": {{Latency: 8 * time.Millisecond, Status: 200, FinalURL: "https://ustc.example/pip/"}, {Latency: 9 * time.Millisecond, Status: 200, FinalURL: "https://ustc.example/pip/"}, {Latency: 7 * time.Millisecond, Status: 200, FinalURL: "https://ustc.example/pip/"}},
	}}
	rows, err := (Engine{Prober: prober, Runs: 3}).Run(context.Background(), model.Environment{}, fakeAdapter{}, []model.Selection{{Mirror: "auto", Endpoint: "https://auto.example"}, {Mirror: "ustc", Endpoint: "https://ustc.example"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Median != 20*time.Millisecond || !rows[1].Fastest || rows[0].Fastest {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}

func TestFailedCandidateIsNeverFastest(t *testing.T) {
	prober := &sequenceProber{index: map[string]int{}, results: map[string][]resolver.ProbeResult{}, errors: map[string]error{"https://bad.example/pip/": errors.New("offline")}}
	rows, err := (Engine{Prober: prober, Runs: 2}).Run(context.Background(), model.Environment{}, fakeAdapter{}, []model.Selection{{Mirror: "bad", Endpoint: "https://bad.example"}})
	if err != nil || len(rows) != 1 || rows[0].Fastest || rows[0].Success != 0 {
		t.Fatalf("rows=%#v err=%v", rows, err)
	}
}

func TestRunReusesIdenticalCandidateEndpoint(t *testing.T) {
	prober := &sequenceProber{index: map[string]int{}, results: map[string][]resolver.ProbeResult{
		"https://same.example/pip/": {{Latency: 12 * time.Millisecond, Status: 200, FinalURL: "https://same.example/pip/"}},
	}}
	rows, err := (Engine{Prober: prober, Runs: 1}).Run(context.Background(), model.Environment{}, fakeAdapter{}, []model.Selection{{Mirror: "auto", Endpoint: "https://same.example"}, {Mirror: "named", Endpoint: "https://same.example"}})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%#v err=%v", rows, err)
	}
	if prober.index["https://same.example/pip/"] != 1 || rows[0].Median != rows[1].Median {
		t.Fatalf("duplicate endpoint was probed more than once: rows=%#v calls=%d", rows, prober.index["https://same.example/pip/"])
	}
}

func TestNonRankableCapabilityNeverClaimsFastest(t *testing.T) {
	prober := &sequenceProber{index: map[string]int{}, results: map[string][]resolver.ProbeResult{
		"https://apt.example": {{Latency: time.Millisecond, Status: 200, FinalURL: "https://apt.example"}},
	}}
	rows, err := (Engine{Prober: prober, Runs: 1}).Run(context.Background(), model.Environment{}, nonRankableAdapter{}, []model.Selection{{Mirror: "auto", Endpoint: "https://apt.example"}})
	if err != nil || len(rows) != 1 || rows[0].Fastest {
		t.Fatalf("rows=%#v err=%v", rows, err)
	}
}
