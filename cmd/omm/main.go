package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chaogao512/oh-my-mirrorz/internal/adapters"
	"github.com/chaogao512/oh-my-mirrorz/internal/adapters/apt"
	"github.com/chaogao512/oh-my-mirrorz/internal/adapters/cargo"
	"github.com/chaogao512/oh-my-mirrorz/internal/adapters/conda"
	"github.com/chaogao512/oh-my-mirrorz/internal/adapters/homebrew"
	"github.com/chaogao512/oh-my-mirrorz/internal/adapters/npm"
	"github.com/chaogao512/oh-my-mirrorz/internal/adapters/pypi"
	appcore "github.com/chaogao512/oh-my-mirrorz/internal/app"
	benchmarkcore "github.com/chaogao512/oh-my-mirrorz/internal/benchmark"
	"github.com/chaogao512/oh-my-mirrorz/internal/model"
	"github.com/chaogao512/oh-my-mirrorz/internal/resolver"
	"github.com/chaogao512/oh-my-mirrorz/internal/runtimeenv"
	"github.com/chaogao512/oh-my-mirrorz/internal/safeurl"
	"github.com/chaogao512/oh-my-mirrorz/internal/state"
	"github.com/chaogao512/oh-my-mirrorz/internal/transaction"
	"github.com/chaogao512/oh-my-mirrorz/internal/version"
)

const usage = `oh-my-mirrorz - safe, reversible mirror switching

Usage:
  omm scan
  omm switch [--dry-run] [--strategy auto|fixed|prefer] [--mirror NAME]
  omm status
  omm mirrors
  omm benchmark [--adapter NAME] [--runs N]
  omm history
  omm restore [SNAPSHOT-ID]
  omm doctor
  omm version

Run "omm <command> -h" for command options.
`

var adapterIDs = []string{"pypi", "npm", "cargo", "homebrew", "apt", "conda"}

func main() {
	os.Exit(run(context.Background(), os.Args[1:]))
}

func run(ctx context.Context, args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(usage)
		return 0
	}
	switch args[0] {
	case "version":
		fmt.Printf("oh-my-mirrorz %s (%s, %s)\n", version.Version, version.Commit, version.Date)
		return 0
	case "scan", "status":
		return runScan(ctx, args[1:])
	case "switch":
		return runSwitch(ctx, args[1:])
	case "mirrors":
		return runMirrors(args[1:])
	case "benchmark":
		return runBenchmark(ctx, args[1:])
	case "history":
		return runHistory(args[1:])
	case "restore":
		return runRestore(args[1:])
	case "doctor":
		return runDoctor(ctx, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func newApp(includeSystem, includeSecurity, allowPrivate bool) (*appcore.App, error) {
	env, err := runtimeenv.Detect(includeSystem, includeSecurity)
	if err != nil {
		return nil, err
	}
	r := resolver.New()
	r.AllowPrivate = allowPrivate
	client := safeHTTPClient(allowPrivate)
	registry := adapters.NewRegistry(
		pypi.New(pypi.WithHTTPClient(client), pypi.WithNetworkVerification(true)),
		npm.New(npm.WithHTTPClient(client), npm.WithNetworkVerification(true)),
		cargo.New(cargo.WithHTTPClient(client), cargo.WithNetworkVerification(true)),
		homebrew.New(homebrew.WithHTTPClient(client), homebrew.WithNetworkVerification(true)),
		apt.New(apt.WithHTTPClient(client), apt.WithNetworkVerification(true)),
		conda.New(conda.WithHTTPClient(client), conda.WithNetworkVerification(true)),
	)
	return &appcore.App{
		Env:      env,
		Registry: registry,
		Resolver: r,
		Prober:   resolver.HTTPProber{Client: client, AllowPrivate: allowPrivate},
		Store:    state.New(filepath.Join(env.XDGStateHome, "oh-my-mirrorz")),
		Writer:   transaction.PrivilegedWriter{SystemRoot: env.SystemRoot},
		Out:      os.Stdout,
	}, nil
}

func safeHTTPClient(allowPrivate bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = safeurl.DialContext(allowPrivate)
	return &http.Client{
		Timeout:   12 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return safeurl.Validate(req.URL.String(), allowPrivate)
		},
	}
}

func runScan(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	includeSystem := flags.Bool("system", false, "include system-level adapters")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return printError(fmt.Errorf("scan accepts no positional arguments"))
	}
	app, err := newApp(*includeSystem, false, false)
	if err != nil {
		return printError(err)
	}
	detections, err := app.Scan(ctx)
	if err != nil {
		return printError(err)
	}
	fmt.Printf("System: %s/%s\n", app.Env.GOOS, app.Env.GOARCH)
	for _, detection := range detections {
		line := fmt.Sprintf("%-10s %-16s scope=%s", detection.AdapterID, detection.Status, detection.Scope)
		if detection.Reason != "" {
			line += "  " + detection.Reason
		}
		fmt.Println(line)
		for _, path := range detection.ConfigPaths {
			fmt.Printf("  %s\n", path)
		}
	}
	return 0
}

func runSwitch(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("switch", flag.ContinueOnError)
	strategyValue := flags.String("strategy", "auto", "auto, fixed, or prefer")
	mirror := flags.String("mirror", "", "mirror name for fixed/prefer")
	prefer := flags.String("prefer", "", "prefer this mirror, then fall back to auto")
	only := flags.String("only", "", "comma-separated adapters")
	exclude := flags.String("exclude", "", "comma-separated adapters")
	dryRun := flags.Bool("dry-run", false, "show the plan without writing")
	yes := flags.Bool("yes", false, "apply without interactive confirmation")
	includeSystem := flags.Bool("system", false, "include system-level adapters")
	includeSecurity := flags.Bool("include-security", false, "allow replacing Ubuntu security sources")
	allowPrivate := flags.Bool("allow-private", false, "allow explicitly selected private endpoints")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return printError(fmt.Errorf("switch accepts no positional arguments"))
	}
	if *includeSecurity && !*includeSystem {
		return printError(fmt.Errorf("--include-security requires --system"))
	}
	if *prefer != "" {
		if *mirror != "" || *strategyValue != "auto" {
			return printError(fmt.Errorf("--prefer conflicts with --mirror/--strategy"))
		}
		*strategyValue = "prefer"
		*mirror = *prefer
	} else if *mirror != "" && *strategyValue == "auto" {
		*strategyValue = "fixed"
	}
	strategy, err := model.ParseStrategy(*strategyValue)
	if err != nil {
		return printError(err)
	}
	app, err := newApp(*includeSystem, *includeSecurity, *allowPrivate)
	if err != nil {
		return printError(err)
	}
	if !*dryRun && !*yes {
		info, statErr := os.Stdin.Stat()
		if statErr != nil || info.Mode()&os.ModeCharDevice == 0 {
			return printError(fmt.Errorf("non-interactive input requires --yes"))
		}
	}
	confirm := func(appcore.Plan) bool {
		if *yes {
			return true
		}
		fmt.Print("Apply this plan? [y/N] ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		return answer == "y" || answer == "yes"
	}
	result := app.Switch(ctx, appcore.SwitchOptions{
		Strategy: strategy,
		Mirror:   *mirror,
		Only:     normalizeAdapterList(splitList(*only)),
		Exclude:  normalizeAdapterList(splitList(*exclude)),
		DryRun:   *dryRun,
		Confirm:  confirm,
	})
	if result.Err != nil {
		return printError(result.Err)
	}
	if result.Manifest != nil {
		fmt.Printf("Committed transaction %s\n", result.Manifest.ID)
	}
	return 0
}

func runMirrors(args []string) int {
	flags := flag.NewFlagSet("mirrors", flag.ContinueOnError)
	adapterID := flags.String("adapter", "", "limit to one adapter")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return printError(fmt.Errorf("mirrors accepts no positional arguments"))
	}
	*adapterID = normalizeAdapterID(*adapterID)
	if *adapterID != "" {
		known := knownAdapters()
		if !known[*adapterID] {
			return printError(fmt.Errorf("unknown adapter %q", *adapterID))
		}
	}
	catalog := resolver.BuiltInCatalog()
	for _, id := range adapterIDs {
		if *adapterID != "" && id != *adapterID {
			continue
		}
		fmt.Printf("%-10s %s\n", id, strings.Join(catalog.Mirrors(id), ", "))
	}
	return 0
}

func runBenchmark(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	adapterID := flags.String("adapter", "", "limit to one adapter")
	runs := flags.Int("runs", 3, "number of probes per candidate")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return printError(fmt.Errorf("benchmark accepts no positional arguments"))
	}
	if *runs < 1 || *runs > 10 {
		return printError(fmt.Errorf("--runs must be between 1 and 10"))
	}
	*adapterID = normalizeAdapterID(*adapterID)
	if *adapterID != "" && !knownAdapters()[*adapterID] {
		return printError(fmt.Errorf("unknown adapter %q", *adapterID))
	}
	app, err := newApp(false, false, false)
	if err != nil {
		return printError(err)
	}
	engine := benchmarkcore.Engine{Prober: app.Prober, Runs: *runs}
	fmt.Printf("%-10s %-16s %-11s %-30s %-9s %-8s %s\n", "ADAPTER", "REPOSITORY", "CANDIDATE", "FINAL TARGET", "MEDIAN", "SUCCESS", "RESULT")
	for _, id := range adapterIDs {
		if *adapterID != "" && id != *adapterID {
			continue
		}
		adapter, ok := app.Registry.Get(id)
		if !ok {
			continue
		}
		candidates, err := app.Resolver.Candidates(id)
		if err != nil {
			fmt.Printf("%-10s unavailable: %v\n", id, err)
			continue
		}
		rows, err := engine.Run(ctx, app.Env, adapter, candidates)
		if err != nil {
			fmt.Printf("%-10s unavailable: %v\n", id, err)
			continue
		}
		if len(rows) == 0 {
			fmt.Printf("%-10s no public repository is active in the current configuration\n", id)
			continue
		}
		for _, row := range rows {
			result := "healthy"
			if row.Success == 0 {
				result = "unreachable"
			} else if row.Success < row.Runs {
				result = "degraded"
			} else if row.Fastest && row.Candidate == "auto" {
				result = "fastest (dynamic)"
			} else if row.Fastest {
				result = "fastest (sample)"
			} else if row.Candidate == "auto" {
				result = "dynamic"
			}
			latency := "-"
			if row.Median > 0 {
				latency = row.Median.Round(time.Millisecond).String()
			}
			fmt.Printf("%-10s %-16s %-11s %-30s %-9s %d/%-6d %s\n", row.AdapterID, row.Capability, row.Candidate, finalTarget(row.FinalURL), latency, row.Success, row.Runs, result)
		}
	}
	fmt.Println("Note: fastest (sample) means the lowest median response latency in this run, not guaranteed download throughput.")
	return 0
}

func runHistory(args []string) int {
	flags := flag.NewFlagSet("history", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return printError(fmt.Errorf("history accepts no positional arguments"))
	}
	app, err := newApp(false, false, false)
	if err != nil {
		return printError(err)
	}
	history, err := app.History()
	if err != nil {
		return printError(err)
	}
	for _, item := range history {
		fmt.Printf("%s  %-12s %-13s %s\n", item.ID, item.Kind, item.Status, item.CreatedAt.Local().Format(time.RFC3339))
	}
	return 0
}

func runRestore(args []string) int {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 1 {
		return printError(fmt.Errorf("restore accepts at most one snapshot ID"))
	}
	app, err := newApp(false, false, false)
	if err != nil {
		return printError(err)
	}
	id := ""
	if flags.NArg() == 1 {
		id = flags.Arg(0)
	}
	result := app.Restore(id)
	if result.Err != nil {
		return printError(result.Err)
	}
	if result.NoOp {
		fmt.Printf("Already at snapshot %s; no files changed.\n", result.Manifest.ID)
		return 0
	}
	fmt.Printf("Restore committed as transaction %s\n", result.Manifest.ID)
	return 0
}

func runDoctor(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return printError(fmt.Errorf("doctor accepts no positional arguments"))
	}
	app, err := newApp(false, false, false)
	if err == nil {
		err = app.Doctor(ctx)
	}
	if err != nil {
		return printError(err)
	}
	fmt.Println("No invalid configuration or unfinished transaction was found.")
	return 0
}

func knownAdapters() map[string]bool {
	result := make(map[string]bool, len(adapterIDs))
	for _, id := range adapterIDs {
		result[id] = true
	}
	return result
}

func finalTarget(value string) string {
	if value == "" {
		return "-"
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return safeurl.Redact(value)
}

func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func normalizeAdapterID(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "pip", "uv", "python", "pypi":
		return "pypi"
	case "brew", "homebrew":
		return "homebrew"
	case "conda", "mamba", "micromamba":
		return "conda"
	default:
		return strings.ToLower(strings.TrimSpace(id))
	}
}

func normalizeAdapterList(ids []string) []string {
	result := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = normalizeAdapterID(id)
		if id != "" && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}

func printError(err error) int {
	fmt.Fprintln(os.Stderr, "error:", err)
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "unknown"), strings.Contains(message, "conflict"), strings.Contains(message, "requires"):
		return 2
	case strings.Contains(message, "invalid configuration"):
		return 3
	case strings.Contains(message, "endpoint"), strings.Contains(message, "network"), strings.Contains(message, "probe"):
		return 4
	case strings.Contains(message, "permission"), strings.Contains(message, "denied"):
		return 5
	case strings.Contains(message, "degraded") || strings.Contains(message, "rollback:"):
		return 7
	case strings.Contains(message, "rolled back"):
		return 6
	case strings.Contains(message, "transaction") || strings.Contains(message, "lock"):
		return 8
	default:
		return 3
	}
}
