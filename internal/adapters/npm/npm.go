// Package npm manages only the unscoped user registry in ~/.npmrc.  Scoped
// registries and authentication lines deliberately remain untouched.
package npm

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

const adapterID = "npm"

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
func (a *Adapter) ID() string           { return adapterID }
func path(env model.Environment) string { return filepath.Join(env.Home, ".npmrc") }

func (a *Adapter) Detect(_ context.Context, env model.Environment) model.Detection {
	if env.NPMConfigOverride {
		return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeUser, Reason: "npm environment variables override the user configuration; unset them before switching"}
	}
	p := path(env)
	if _, _, exists, err := adapterinternal.Read(p); err != nil {
		return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeUser, Reason: "cannot read .npmrc"}
	} else if exists {
		return model.Detection{AdapterID: adapterID, Status: model.StatusDetected, Scope: model.ScopeUser, ConfigPaths: []string{p}}
	}
	if _, err := exec.LookPath("npm"); err == nil {
		return model.Detection{AdapterID: adapterID, Status: model.StatusDetected, Scope: model.ScopeUser}
	}
	return model.Detection{AdapterID: adapterID, Status: model.StatusNotInstalled, Scope: model.ScopeUser, Reason: "npm executable and user configuration were not found"}
}

func (a *Adapter) Inspect(_ context.Context, env model.Environment) ([]byte, error) {
	b, _, _, err := adapterinternal.Read(path(env))
	return b, err
}

func endpoint(selection model.Selection) (string, error) {
	if selection.Endpoints != nil && selection.Endpoints["npm"] != "" {
		return adapterinternal.Endpoint(selection.Endpoints["npm"])
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
	value, err := endpoint(selection)
	if err != nil {
		return nil, err
	}
	p := path(env)
	before, _, _, err := adapterinternal.Read(p)
	if err != nil {
		return nil, err
	}
	after := setRegistry(before, value)
	change, err := adapterinternal.Change(adapterID, p, model.ScopeUser, after)
	if err != nil {
		return nil, err
	}
	return []model.Change{change}, nil
}

func (a *Adapter) ProbeTargets(_ model.Environment, selection model.Selection) ([]model.ProbeTarget, error) {
	value, err := endpoint(selection)
	if err != nil {
		return nil, err
	}
	return []model.ProbeTarget{{Capability: "registry", URL: strings.TrimRight(value, "/") + "/-/ping", Rankable: true}}, nil
}

func setRegistry(before []byte, registry string) []byte {
	if len(before) == 0 {
		return []byte("registry=" + registry + "\n")
	}
	lines := strings.Split(strings.TrimSuffix(string(before), "\n"), "\n")
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "@") {
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "registry" {
			lines[i] = "registry=" + registry
			changed = true
		}
	}
	if !changed {
		lines = append(lines, "registry="+registry)
	}
	return adapterinternal.JoinLines(lines, true)
}

func (a *Adapter) Verify(_ context.Context, env model.Environment, selection model.Selection) model.Verification {
	value, err := endpoint(selection)
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	b, _, exists, err := adapterinternal.Read(path(env))
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	if !exists || !hasRegistry(b, value) {
		return model.Verification{AdapterID: adapterID, Detail: "npm registry does not match selected endpoint"}
	}
	if a.networkVerify {
		if err := verifyPing(a.client, value); err != nil {
			return model.Verification{AdapterID: adapterID, Detail: err.Error()}
		}
	}
	return model.Verification{AdapterID: adapterID, OK: true, Detail: "npm registry matches selected endpoint"}
}

func verifyPing(client *http.Client, registry string) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(registry, "/")+"/-/ping", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "oh-my-mirrorz/0.2")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("npm ping returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func hasRegistry(b []byte, registry string) bool {
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "@") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if ok && strings.TrimSpace(key) == "registry" && strings.TrimRight(strings.TrimSpace(value), "/") == strings.TrimRight(registry, "/") {
			return true
		}
	}
	return false
}
