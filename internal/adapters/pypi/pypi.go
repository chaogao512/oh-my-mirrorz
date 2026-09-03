// Package pypi manages user-level pip and uv indexes.  It intentionally does
// not touch project configuration (pyproject.toml, Poetry or PDM files).
package pypi

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

const adapterID = "pypi"

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

func paths(env model.Environment) (string, string) {
	pip := filepath.Join(adapterinternal.XDGConfig(env), "pip", "pip.conf")
	if env.GOOS == "darwin" {
		pip = filepath.Join(env.Home, "Library", "Application Support", "pip", "pip.conf")
	}
	return pip, filepath.Join(adapterinternal.XDGConfig(env), "uv", "uv.toml")
}

func pipConfigPaths(env model.Environment) []string {
	preferred, _ := paths(env)
	legacy := filepath.Join(env.Home, ".pip", "pip.conf")
	if preferred == legacy {
		return []string{preferred}
	}
	return []string{preferred, legacy}
}

func (a *Adapter) Detect(_ context.Context, env model.Environment) model.Detection {
	if env.PipConfigOverride || env.PipIndexOverride || env.UVIndexOverride {
		return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeUser, Reason: "pip/uv environment variables override user configuration; unset them before switching"}
	}
	_, uv := paths(env)
	var found []string
	for _, path := range append(pipConfigPaths(env), uv) {
		if _, _, exists, err := adapterinternal.Read(path); err == nil && exists {
			found = append(found, path)
		}
	}
	if len(found) > 0 {
		return model.Detection{AdapterID: adapterID, Status: model.StatusDetected, Scope: model.ScopeUser, ConfigPaths: found}
	}
	if _, err := exec.LookPath("pip"); err == nil {
		return model.Detection{AdapterID: adapterID, Status: model.StatusDetected, Scope: model.ScopeUser}
	}
	if _, err := exec.LookPath("uv"); err == nil {
		return model.Detection{AdapterID: adapterID, Status: model.StatusDetected, Scope: model.ScopeUser}
	}
	return model.Detection{AdapterID: adapterID, Status: model.StatusNotInstalled, Scope: model.ScopeUser, Reason: "neither pip nor uv user configuration or executable was found"}
}

func (a *Adapter) Inspect(_ context.Context, env model.Environment) ([]byte, error) {
	_, uv := paths(env)
	var result []string
	for _, path := range append(pipConfigPaths(env), uv) {
		b, _, exists, err := adapterinternal.Read(path)
		if err != nil {
			return nil, err
		}
		if exists {
			result = append(result, "# "+path+"\n"+string(b))
		}
	}
	return []byte(strings.Join(result, "\n")), nil
}

func endpoint(selection model.Selection, names ...string) (string, error) {
	for _, name := range names {
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
	value, err := endpoint(selection, "pypi", "pip", "uv")
	if err != nil {
		return nil, err
	}
	pipPath, uvPath := paths(env)
	changes := make([]model.Change, 0, 3)
	pipTargets := []string{}
	for _, candidate := range pipConfigPaths(env) {
		_, _, exists, readErr := adapterinternal.Read(candidate)
		if readErr != nil {
			return nil, readErr
		}
		if exists {
			pipTargets = append(pipTargets, candidate)
		}
	}
	_, _, uvConfig, uvReadErr := adapterinternal.Read(uvPath)
	if uvReadErr != nil {
		return nil, uvReadErr
	}
	_, pipLookErr := exec.LookPath("pip")
	_, uvLookErr := exec.LookPath("uv")
	pipInstalled, uvInstalled := pipLookErr == nil, uvLookErr == nil
	if len(pipTargets) == 0 && !uvConfig && !pipInstalled && !uvInstalled {
		return nil, nil
	}
	if len(pipTargets) == 0 && pipInstalled {
		pipTargets = append(pipTargets, pipPath)
	}
	for _, target := range pipTargets {
		before, _, _, readErr := adapterinternal.Read(target)
		if readErr != nil {
			return nil, readErr
		}
		after, err := setPipIndex(before, value)
		if err != nil {
			return nil, err
		}
		change, err := adapterinternal.Change(adapterID, target, model.ScopeUser, after)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	if uvConfig || uvInstalled {
		before, _, _, readErr := adapterinternal.Read(uvPath)
		if readErr != nil {
			return nil, readErr
		}
		after, err := setUVIndex(before, value)
		if err != nil {
			return nil, err
		}
		change, err := adapterinternal.Change(adapterID, uvPath, model.ScopeUser, after)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func setPipIndex(before []byte, endpoint string) ([]byte, error) {
	lines := strings.Split(strings.TrimSuffix(string(before), "\n"), "\n")
	if len(before) == 0 {
		return []byte("[global]\nindex-url = " + endpoint + "\n"), nil
	}
	inGlobal, changed := false, false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inGlobal = strings.EqualFold(strings.TrimSpace(trimmed[1:len(trimmed)-1]), "global")
			continue
		}
		if inGlobal && keyIs(trimmed, "index-url") {
			lines[i] = "index-url = " + endpoint
			changed = true
		}
	}
	if !changed {
		global := -1
		for i, line := range lines {
			if strings.EqualFold(strings.TrimSpace(line), "[global]") {
				global = i
				break
			}
		}
		if global >= 0 {
			lines = append(lines, "")
			copy(lines[global+2:], lines[global+1:])
			lines[global+1] = "index-url = " + endpoint
		} else {
			lines = append(lines, "", "[global]", "index-url = "+endpoint)
		}
	}
	return adapterinternal.JoinLines(lines, true), nil
}

func keyIs(line, key string) bool {
	if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
		return false
	}
	left, _, ok := strings.Cut(line, "=")
	if !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(left), key)
}

func setUVIndex(before []byte, endpoint string) ([]byte, error) {
	if len(before) == 0 {
		return []byte("[[index]]\nurl = \"" + endpoint + "\"\ndefault = true\n"), nil
	}
	lines := strings.Split(strings.TrimSuffix(string(before), "\n"), "\n")
	inIndex, changed, defaultIndex := false, false, -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[[index]]" {
			inIndex = true
			if defaultIndex == -1 {
				defaultIndex = i
			}
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inIndex = false
			continue
		}
		if inIndex && keyIs(trimmed, "default") && strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.SplitN(trimmed, "=", 2)[1]), "\"")), "true") {
			defaultIndex = i // mark this table by locating its header below
			for j := i; j >= 0; j-- {
				if strings.TrimSpace(lines[j]) == "[[index]]" {
					defaultIndex = j
					break
				}
			}
		}
	}
	if defaultIndex >= 0 {
		end := len(lines)
		for i := defaultIndex + 1; i < len(lines); i++ {
			s := strings.TrimSpace(lines[i])
			if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
				end = i
				break
			}
		}
		for i := defaultIndex + 1; i < end; i++ {
			if keyIs(strings.TrimSpace(lines[i]), "url") {
				lines[i] = "url = \"" + endpoint + "\""
				changed = true
			}
		}
		if !changed {
			lines = append(lines, "")
			copy(lines[end+1:], lines[end:])
			lines[end] = "url = \"" + endpoint + "\""
		}
		// Ensure the selected source becomes the default even when an existing
		// index table omitted that field.
		hasDefault := false
		end = len(lines)
		for i := defaultIndex + 1; i < len(lines); i++ {
			s := strings.TrimSpace(lines[i])
			if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
				end = i
				break
			}
			if keyIs(s, "default") {
				hasDefault = true
				lines[i] = "default = true"
			}
		}
		if !hasDefault {
			lines = append(lines, "")
			copy(lines[end+1:], lines[end:])
			lines[end] = "default = true"
		}
	} else {
		lines = append(lines, "", "[[index]]", "url = \""+endpoint+"\"", "default = true")
	}
	return adapterinternal.JoinLines(lines, true), nil
}

func (a *Adapter) Verify(_ context.Context, env model.Environment, selection model.Selection) model.Verification {
	value, err := endpoint(selection, "pypi", "pip", "uv")
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	_, uv := paths(env)
	for _, path := range append(pipConfigPaths(env), uv) {
		b, _, exists, readErr := adapterinternal.Read(path)
		if readErr != nil {
			return model.Verification{AdapterID: adapterID, Detail: readErr.Error()}
		}
		if exists && !strings.Contains(string(b), value) {
			return model.Verification{AdapterID: adapterID, Detail: "configured index does not match selected endpoint"}
		}
		if exists && path == uv && (!strings.Contains(string(b), "[[index]]") || !strings.Contains(string(b), "default = true")) {
			return model.Verification{AdapterID: adapterID, Detail: "uv default index is not configured"}
		}
	}
	if a.networkVerify {
		if err := adapterinternal.VerifyEndpoint(a.client, strings.TrimRight(value, "/")+"/pip/"); err != nil {
			return model.Verification{AdapterID: adapterID, Detail: err.Error()}
		}
	}
	return model.Verification{AdapterID: adapterID, OK: true, Detail: "pip/uv configuration matches selected endpoint"}
}
