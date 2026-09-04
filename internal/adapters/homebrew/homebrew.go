// Package homebrew adds a narrow, removable managed block to Homebrew's
// user-level brew.env file. It never rewrites settings outside that block.
package homebrew

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	adapterinternal "github.com/chaogao512/oh-my-mirrorz/internal/adapters/internal"
	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

const adapterID = "homebrew"
const begin = "# >>> oh-my-mirrorz managed block >>>"
const end = "# <<< oh-my-mirrorz managed block <<<"

type Adapter struct {
	client                 *http.Client
	networkVerify, brewGit bool
	coreGitTap             bool
}
type Option func(*Adapter)

func WithHTTPClient(c *http.Client) Option { return func(a *Adapter) { a.client = c } }
func WithNetworkVerification(enabled bool) Option {
	return func(a *Adapter) { a.networkVerify = enabled }
}

// WithBrewGitRemote is intentionally opt-in. Once Homebrew updates its own
// Git origin, removing brew.env does not restore that origin; the CLI therefore
// leaves this disabled until adapter-aware remote restoration is available.
func WithBrewGitRemote(enabled bool) Option { return func(a *Adapter) { a.brewGit = enabled } }

// WithCoreGitTap should be supplied by the environment scanner after it has
// verified that homebrew/core is an actual Git tap.
func WithCoreGitTap(enabled bool) Option { return func(a *Adapter) { a.coreGitTap = enabled } }
func New(options ...Option) *Adapter {
	a := &Adapter{}
	for _, option := range options {
		option(a)
	}
	return a
}
func (a *Adapter) ID() string { return adapterID }

func configPath(env model.Environment) string {
	home := env.HomebrewConfigHome
	if home == "" {
		home = filepath.Join(adapterinternal.XDGConfig(env), "homebrew")
	}
	modern := filepath.Join(home, "brew.env")
	if _, _, exists, _ := adapterinternal.Read(modern); exists {
		return modern
	}
	legacy := filepath.Join(env.Home, ".homebrew", "brew.env")
	if _, _, exists, _ := adapterinternal.Read(legacy); exists {
		return legacy
	}
	return modern
}
func (a *Adapter) Detect(_ context.Context, env model.Environment) model.Detection {
	p := configPath(env)
	if _, _, exists, err := adapterinternal.Read(p); err != nil {
		return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeUser, Reason: "cannot read Homebrew environment file"}
	} else if exists {
		return model.Detection{AdapterID: adapterID, Status: model.StatusDetected, Scope: model.ScopeUser, ConfigPaths: []string{p}}
	}
	if _, err := exec.LookPath("brew"); err == nil {
		return model.Detection{AdapterID: adapterID, Status: model.StatusDetected, Scope: model.ScopeUser}
	}
	return model.Detection{AdapterID: adapterID, Status: model.StatusNotInstalled, Scope: model.ScopeUser, Reason: "Homebrew executable and environment configuration were not found"}
}
func (a *Adapter) Inspect(_ context.Context, env model.Environment) ([]byte, error) {
	p := configPath(env)
	b, _, _, err := adapterinternal.Read(p)
	return b, err
}

func selected(selection model.Selection, key string) (string, error) {
	aliases := map[string][]string{
		"brew":   {"brew_git", "homebrew_brew_git_remote", "brew_git_remote", "brew"},
		"api":    {"api", "homebrew_api_domain", "api_domain"},
		"bottle": {"bottles", "bottle", "homebrew_bottle_domain", "bottle_domain"},
		"pip":    {"pypi", "homebrew_pip_index_url", "pip_index_url"},
		"core":   {"core_git", "homebrew_core_git_remote", "core_git_remote", "core"},
	}
	for _, name := range aliases[key] {
		if selection.Endpoints != nil && selection.Endpoints[name] != "" {
			return adapterinternal.Endpoint(selection.Endpoints[name])
		}
	}
	return adapterinternal.Endpoint(selection.Endpoint)
}

func (a *Adapter) Plan(_ context.Context, env model.Environment, selection model.Selection) ([]model.Change, error) {
	if selection.AdapterID != "" && selection.AdapterID != adapterID {
		return nil, fmt.Errorf("selection is for %q, not %q", selection.AdapterID, adapterID)
	}
	if a.Detect(context.Background(), env).Status != model.StatusDetected {
		return nil, nil
	}
	p := configPath(env)
	values := map[string]string{}
	for _, key := range []string{"api", "bottle", "pip"} {
		v, err := selected(selection, key)
		if err != nil {
			return nil, err
		}
		values[key] = strings.TrimRight(v, "/")
	}
	if a.brewGit {
		v, err := selected(selection, "brew")
		if err != nil {
			return nil, err
		}
		values["brew"] = strings.TrimRight(v, "/")
	}
	if a.coreGitTap {
		v, err := selected(selection, "core")
		if err != nil {
			return nil, err
		}
		values["core"] = strings.TrimRight(v, "/")
	}
	before, _, _, err := adapterinternal.Read(p)
	if err != nil {
		return nil, err
	}
	after, err := replaceBlock(before, block(values))
	if err != nil {
		return nil, err
	}
	change, err := adapterinternal.Change(adapterID, p, model.ScopeUser, after)
	if err != nil {
		return nil, err
	}
	return []model.Change{change}, nil
}

func (a *Adapter) ProbeTargets(_ model.Environment, selection model.Selection) ([]model.ProbeTarget, error) {
	api, err := selected(selection, "api")
	if err != nil {
		return nil, err
	}
	bottles, err := selected(selection, "bottle")
	if err != nil {
		return nil, err
	}
	pip, err := selected(selection, "pip")
	if err != nil {
		return nil, err
	}
	return []model.ProbeTarget{
		{Capability: "api", URL: strings.TrimRight(api, "/") + "/formula.jws.json", Rankable: true},
		{Capability: "bottles", URL: strings.TrimRight(bottles, "/") + "/", Rankable: true},
		{Capability: "build-pypi", URL: strings.TrimRight(pip, "/") + "/pip/", Rankable: true},
	}, nil
}

func block(values map[string]string) string {
	// brew.env is parsed as literal KEY=VALUE lines by Homebrew's launcher;
	// shell quotes would become part of the endpoint value.
	lines := []string{begin, "# Managed by oh-my-mirrorz; remove this block to restore Homebrew defaults."}
	if values["brew"] != "" {
		lines = append(lines, "HOMEBREW_BREW_GIT_REMOTE="+values["brew"])
	}
	lines = append(lines, "HOMEBREW_API_DOMAIN="+values["api"], "HOMEBREW_BOTTLE_DOMAIN="+values["bottle"], "HOMEBREW_PIP_INDEX_URL="+values["pip"])
	if values["core"] != "" {
		lines = append(lines, "HOMEBREW_CORE_GIT_REMOTE="+values["core"])
	}
	lines = append(lines, end)
	return strings.Join(lines, "\n")
}

func replaceBlock(before []byte, replacement string) ([]byte, error) {
	lines := strings.Split(strings.TrimSuffix(string(before), "\n"), "\n")
	if len(before) == 0 {
		return []byte(replacement + "\n"), nil
	}
	result, inBlock, removed := make([]string, 0, len(lines)+8), false, false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case begin:
			if inBlock {
				return nil, fmt.Errorf("nested Homebrew managed block")
			}
			inBlock, removed = true, true
		case end:
			if !inBlock {
				return nil, fmt.Errorf("orphan Homebrew managed block end marker")
			}
			inBlock = false
		default:
			if !inBlock {
				result = append(result, line)
			}
		}
	}
	if inBlock {
		return nil, fmt.Errorf("unterminated Homebrew managed block")
	}
	if len(result) > 0 && strings.TrimSpace(result[len(result)-1]) != "" {
		result = append(result, "")
	}
	result = append(result, strings.Split(replacement, "\n")...)
	if removed {
		return adapterinternal.JoinLines(result, true), nil
	}
	return adapterinternal.JoinLines(result, true), nil
}

func (a *Adapter) Verify(_ context.Context, env model.Environment, selection model.Selection) model.Verification {
	p := configPath(env)
	b, _, exists, err := adapterinternal.Read(p)
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	if !exists {
		return model.Verification{AdapterID: adapterID, Detail: "Homebrew environment file was not created"}
	}
	values := map[string]string{}
	for _, key := range []string{"api", "bottle", "pip"} {
		v, err := selected(selection, key)
		if err != nil {
			return model.Verification{AdapterID: adapterID, Detail: err.Error()}
		}
		values[key] = strings.TrimRight(v, "/")
	}
	if a.brewGit {
		v, err := selected(selection, "brew")
		if err != nil {
			return model.Verification{AdapterID: adapterID, Detail: err.Error()}
		}
		values["brew"] = strings.TrimRight(v, "/")
	}
	if a.coreGitTap {
		v, err := selected(selection, "core")
		if err != nil {
			return model.Verification{AdapterID: adapterID, Detail: err.Error()}
		}
		values["core"] = strings.TrimRight(v, "/")
	}
	want := block(values)
	if !strings.Contains(string(b), want) {
		return model.Verification{AdapterID: adapterID, Detail: "Homebrew managed block does not match selected endpoints"}
	}
	if a.networkVerify {
		if err := adapterinternal.VerifyEndpoint(a.client, strings.TrimRight(values["api"], "/")+"/formula.jws.json"); err != nil {
			return model.Verification{AdapterID: adapterID, Detail: err.Error()}
		}
	}
	return model.Verification{AdapterID: adapterID, OK: true, Detail: "Homebrew managed block matches selected endpoints"}
}
