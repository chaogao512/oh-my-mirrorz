// Package cargo manages Cargo source replacement without touching credentials.
package cargo

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	adapterinternal "github.com/chaogao512/oh-my-mirrorz/internal/adapters/internal"
	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

const adapterID = "cargo"
const managedSource = "oh-my-mirrorz"

type Adapter struct {
	client        *http.Client
	networkVerify bool
}
type Option func(*Adapter)

func WithHTTPClient(c *http.Client) Option { return func(a *Adapter) { a.client = c } }
func WithNetworkVerification(enabled bool) Option {
	return func(a *Adapter) { a.networkVerify = enabled }
}
func New(options ...Option) *Adapter {
	a := &Adapter{}
	for _, option := range options {
		option(a)
	}
	return a
}
func (a *Adapter) ID() string { return adapterID }

func configPath(env model.Environment) string {
	home := env.CargoHome
	if home == "" {
		home = filepath.Join(env.Home, ".cargo")
	}
	legacy := filepath.Join(home, "config")
	if _, _, exists, _ := adapterinternal.Read(legacy); exists {
		return legacy
	}
	modern := filepath.Join(home, "config.toml")
	if _, _, exists, _ := adapterinternal.Read(modern); exists {
		return modern
	}
	return modern
}

// projectReplacement walks upward from the current working directory because
// Cargo project configuration has higher precedence than CARGO_HOME.  The
// public Environment deliberately carries no project path, so it is only a
// conflict guard: it never writes outside CARGO_HOME.
func projectReplacement() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		for _, name := range []string{"config", "config.toml"} {
			path := filepath.Join(dir, ".cargo", name)
			if b, _, exists, readErr := adapterinternal.Read(path); readErr == nil && exists {
				if replacement, ok := getSectionKey(b, "source.crates-io", "replace-with"); ok {
					return replacement, true
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
func (a *Adapter) Detect(_ context.Context, env model.Environment) model.Detection {
	p := configPath(env)
	if _, _, exists, err := adapterinternal.Read(p); err != nil {
		return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeUser, Reason: "cannot read Cargo config"}
	} else if exists {
		return model.Detection{AdapterID: adapterID, Status: model.StatusDetected, Scope: model.ScopeUser, ConfigPaths: []string{p}}
	}
	if _, err := exec.LookPath("cargo"); err == nil {
		return model.Detection{AdapterID: adapterID, Status: model.StatusDetected, Scope: model.ScopeUser}
	}
	return model.Detection{AdapterID: adapterID, Status: model.StatusNotInstalled, Scope: model.ScopeUser, Reason: "cargo executable and configuration were not found"}
}
func (a *Adapter) Inspect(_ context.Context, env model.Environment) ([]byte, error) {
	b, _, _, err := adapterinternal.Read(configPath(env))
	return b, err
}

func endpoint(selection model.Selection) (string, error) {
	v := selection.Endpoint
	if selection.Endpoints != nil && selection.Endpoints["cargo"] != "" {
		v = selection.Endpoints["cargo"]
	}
	v = strings.TrimPrefix(v, "sparse+")
	return adapterinternal.Endpoint(v)
}

func (a *Adapter) Plan(_ context.Context, env model.Environment, selection model.Selection) ([]model.Change, error) {
	if selection.AdapterID != "" && selection.AdapterID != adapterID {
		return nil, fmt.Errorf("selection is for %q, not %q", selection.AdapterID, adapterID)
	}
	if a.Detect(context.Background(), env).Status != model.StatusDetected {
		return nil, nil
	}
	value, err := endpoint(selection)
	if err != nil {
		return nil, err
	}
	p := configPath(env)
	before, _, _, err := adapterinternal.Read(p)
	if err != nil {
		return nil, err
	}
	if replacement, ok := getSectionKey(before, "source.crates-io", "replace-with"); ok && replacement != managedSource {
		return nil, fmt.Errorf("Cargo crates-io already replaces with custom source %q; refusing to overwrite it", replacement)
	}
	if replacement, ok := projectReplacement(); ok && replacement != managedSource {
		return nil, fmt.Errorf("project Cargo configuration replaces crates-io with custom source %q; refusing to shadow it", replacement)
	}
	after := setSectionKey(before, "source.crates-io", "replace-with", `"`+managedSource+`"`)
	after = setSectionKey(after, "source."+managedSource, "registry", `"sparse+`+value+`"`)
	change, err := adapterinternal.Change(adapterID, p, model.ScopeUser, after)
	if err != nil {
		return nil, err
	}
	return []model.Change{change}, nil
}

func getSectionKey(b []byte, section, key string) (string, bool) {
	in := false
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			in = strings.TrimSpace(trimmed[1:len(trimmed)-1]) == section
			continue
		}
		if !in || strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, v, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		return strings.Trim(strings.TrimSpace(v), `"`), true
	}
	return "", false
}

func setSectionKey(before []byte, section, key, value string) []byte {
	if len(before) == 0 {
		return []byte("[" + section + "]\n" + key + " = " + value + "\n")
	}
	lines := strings.Split(strings.TrimSuffix(string(before), "\n"), "\n")
	in, changed, end := false, false, len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if in && end == len(lines) {
				end = i
			}
			in = strings.TrimSpace(trimmed[1:len(trimmed)-1]) == section
			continue
		}
		if in && !strings.HasPrefix(trimmed, "#") {
			if k, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(k) == key {
				lines[i] = key + " = " + value
				changed = true
			}
		}
	}
	if changed {
		return adapterinternal.JoinLines(lines, true)
	}
	// Insert inside existing section, before its following table.  Appending to
	// an existing table after another table would silently change TOML meaning.
	found, start := false, -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "["+section+"]" {
			found, start = true, i
			break
		}
	}
	if found {
		insert := len(lines)
		for i := start + 1; i < len(lines); i++ {
			s := strings.TrimSpace(lines[i])
			if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
				insert = i
				break
			}
		}
		lines = append(lines, "")
		copy(lines[insert+1:], lines[insert:])
		lines[insert] = key + " = " + value
	} else {
		lines = append(lines, "", "["+section+"]", key+" = "+value)
	}
	return adapterinternal.JoinLines(lines, true)
}

func (a *Adapter) Verify(_ context.Context, env model.Environment, selection model.Selection) model.Verification {
	value, err := endpoint(selection)
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	b, _, exists, err := adapterinternal.Read(configPath(env))
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	if !exists {
		return model.Verification{AdapterID: adapterID, Detail: "Cargo config was not created"}
	}
	replacement, ok := getSectionKey(b, "source.crates-io", "replace-with")
	registry, registryOK := getSectionKey(b, "source."+managedSource, "registry")
	if !ok || replacement != managedSource || !registryOK || registry != "sparse+"+value {
		return model.Verification{AdapterID: adapterID, Detail: "Cargo source replacement does not match selected endpoint"}
	}
	if a.networkVerify {
		if err := adapterinternal.VerifyEndpoint(a.client, strings.TrimRight(value, "/")+"/config.json"); err != nil {
			return model.Verification{AdapterID: adapterID, Detail: err.Error()}
		}
	}
	return model.Verification{AdapterID: adapterID, OK: true, Detail: "Cargo source replacement matches selected endpoint"}
}
