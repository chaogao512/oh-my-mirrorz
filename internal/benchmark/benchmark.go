// Package benchmark compares catalog candidates through adapter-defined,
// protocol-aware probe targets. It never changes configuration.
package benchmark

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/chaogao512/oh-my-mirrorz/internal/adapters"
	"github.com/chaogao512/oh-my-mirrorz/internal/model"
	"github.com/chaogao512/oh-my-mirrorz/internal/resolver"
)

type Row struct {
	AdapterID  string
	Capability string
	Candidate  string
	ProbeURL   string
	FinalURL   string
	Median     time.Duration
	Success    int
	Runs       int
	Status     int
	Rankable   bool
	Fastest    bool
	Err        error
}

type Engine struct {
	Prober resolver.Prober
	Runs   int
}

type probeOutcome struct {
	finalURL string
	median   time.Duration
	success  int
	status   int
	err      error
}

func (e Engine) Run(ctx context.Context, env model.Environment, adapter adapters.Adapter, candidates []model.Selection) ([]Row, error) {
	if e.Prober == nil {
		return nil, fmt.Errorf("benchmark prober is nil")
	}
	runs := e.Runs
	if runs <= 0 {
		runs = 3
	}
	var rows []Row
	cache := map[string]probeOutcome{}
	for _, candidate := range candidates {
		targets, err := adapter.ProbeTargets(env, candidate)
		if err != nil {
			return nil, fmt.Errorf("%s %s probe targets: %w", adapter.ID(), candidate.Mirror, err)
		}
		for _, target := range targets {
			row := Row{AdapterID: adapter.ID(), Capability: target.Capability, Candidate: candidate.Mirror, ProbeURL: target.URL, Runs: runs, Rankable: target.Rankable}
			if cached, ok := cache[target.URL]; ok {
				row.FinalURL, row.Median, row.Success, row.Status, row.Err = cached.finalURL, cached.median, cached.success, cached.status, cached.err
				rows = append(rows, row)
				continue
			}
			var latencies []time.Duration
			for i := 0; i < runs; i++ {
				result, probeErr := e.Prober.Probe(ctx, target.URL)
				if probeErr != nil {
					row.Err = probeErr
					continue
				}
				row.Success++
				row.Status = result.Status
				if row.FinalURL == "" {
					row.FinalURL = result.FinalURL
				}
				latencies = append(latencies, result.Latency)
			}
			if len(latencies) > 0 {
				sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
				row.Median = latencies[len(latencies)/2]
			}
			cache[target.URL] = probeOutcome{finalURL: row.FinalURL, median: row.Median, success: row.Success, status: row.Status, err: row.Err}
			rows = append(rows, row)
		}
	}
	markFastest(rows)
	return rows, nil
}

func markFastest(rows []Row) {
	type best struct {
		index   int
		latency time.Duration
	}
	byCapability := map[string]best{}
	for i, row := range rows {
		if !row.Rankable || row.Success != row.Runs || row.Median <= 0 {
			continue
		}
		key := row.AdapterID + "\x00" + row.Capability
		current, ok := byCapability[key]
		if !ok || row.Median < current.latency || (row.Median == current.latency && row.Candidate < rows[current.index].Candidate) {
			byCapability[key] = best{index: i, latency: row.Median}
		}
	}
	for _, item := range byCapability {
		rows[item.index].Fastest = true
	}
}
