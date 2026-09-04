// Package conda manages public mirror endpoints in the user-level .condarc.
// It preserves channel selection, ordering, priority, private channels, and
// unrelated settings; project, environment, and root-prefix files are never
// modified.
package conda

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"

	adapterinternal "github.com/chaogao512/oh-my-mirrorz/internal/adapters/internal"
	"github.com/chaogao512/oh-my-mirrorz/internal/model"
	"gopkg.in/yaml.v3"
)

const adapterID = "conda"

var publicChannels = []string{"conda-forge", "pytorch"}

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

func configPath(env model.Environment) string { return filepath.Join(env.Home, ".condarc") }

func shadowPaths(env model.Environment) []string {
	return []string{filepath.Join(env.Home, ".conda", ".condarc"), filepath.Join(env.Home, ".mambarc")}
}

func (a *Adapter) Detect(_ context.Context, env model.Environment) model.Detection {
	if env.CondaConfigOverride {
		return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeUser, Reason: "Conda/Mamba environment variables override channel configuration; unset them before switching"}
	}
	for _, candidate := range shadowPaths(env) {
		content, _, exists, err := adapterinternal.Read(candidate)
		if err != nil {
			return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeUser, Reason: "cannot read additional Conda/Mamba configuration"}
		}
		if !exists {
			continue
		}
		doc, err := parseDocument(content)
		if err != nil {
			return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeUser, ConfigPaths: []string{candidate}, Reason: "additional Conda/Mamba configuration is invalid YAML"}
		}
		if hasMirrorKeys(doc) {
			return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeUser, ConfigPaths: []string{candidate}, Reason: candidate + " also controls channels; consolidate it into ~/.condarc before switching"}
		}
	}
	p := configPath(env)
	if content, _, exists, err := adapterinternal.Read(p); err != nil {
		return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeUser, Reason: "cannot read ~/.condarc"}
	} else if exists {
		if _, err := parseDocument(content); err != nil {
			return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeUser, ConfigPaths: []string{p}, Reason: err.Error()}
		}
		return model.Detection{AdapterID: adapterID, Status: model.StatusDetected, Scope: model.ScopeUser, ConfigPaths: []string{p}}
	}
	for _, name := range []string{"conda", "mamba", "micromamba"} {
		if _, err := exec.LookPath(name); err == nil {
			return model.Detection{AdapterID: adapterID, Status: model.StatusDetected, Scope: model.ScopeUser, Reason: name + " executable found"}
		}
	}
	return model.Detection{AdapterID: adapterID, Status: model.StatusNotInstalled, Scope: model.ScopeUser, Reason: "Conda, Mamba, Micromamba, and ~/.condarc were not found"}
}

func (a *Adapter) Inspect(_ context.Context, env model.Environment) ([]byte, error) {
	content, _, _, err := adapterinternal.Read(configPath(env))
	return content, err
}

func selectedBase(selection model.Selection) (string, error) {
	value := selection.Endpoint
	if selection.Endpoints != nil && selection.Endpoints["anaconda"] != "" {
		value = selection.Endpoints["anaconda"]
	}
	value, err := adapterinternal.Endpoint(value)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(value, "/"), nil
}

func (a *Adapter) Plan(_ context.Context, env model.Environment, selection model.Selection) ([]model.Change, error) {
	if selection.AdapterID != "" && selection.AdapterID != adapterID {
		return nil, fmt.Errorf("selection is for %q, not %q", selection.AdapterID, adapterID)
	}
	if a.Detect(context.Background(), env).Status != model.StatusDetected {
		return nil, nil
	}
	base, err := selectedBase(selection)
	if err != nil {
		return nil, err
	}
	p := configPath(env)
	before, _, _, err := adapterinternal.Read(p)
	if err != nil {
		return nil, err
	}
	doc, err := parseDocument(before)
	if err != nil {
		return nil, err
	}
	current, err := inspectPolicy(doc)
	if err != nil {
		return nil, err
	}
	if !current.defaults && len(current.public) == 0 {
		return nil, nil
	}
	if err := configure(doc, base); err != nil {
		return nil, err
	}
	after, err := encodeDocument(doc)
	if err != nil {
		return nil, err
	}
	change, err := adapterinternal.Change(adapterID, p, model.ScopeUser, after)
	if err != nil {
		return nil, err
	}
	return []model.Change{change}, nil
}

func (a *Adapter) ProbeTargets(env model.Environment, selection model.Selection) ([]model.ProbeTarget, error) {
	base, err := selectedBase(selection)
	if err != nil {
		return nil, err
	}
	content, _, _, err := adapterinternal.Read(configPath(env))
	if err != nil {
		return nil, err
	}
	doc, err := parseDocument(content)
	if err != nil {
		return nil, err
	}
	policy, err := inspectPolicy(doc)
	if err != nil {
		return nil, err
	}
	subdir := platformSubdir(env.GOOS, env.GOARCH)
	var targets []model.ProbeTarget
	if policy.defaults {
		targets = append(targets, model.ProbeTarget{Capability: "defaults", URL: base + "/pkgs/main/" + subdir + "/repodata.json", Rankable: true})
	}
	for _, channel := range publicChannels {
		if policy.public[channel] {
			targets = append(targets, model.ProbeTarget{Capability: channel, URL: base + "/cloud/" + channel + "/" + subdir + "/repodata.json", Rankable: true})
		}
	}
	return targets, nil
}

func (a *Adapter) Verify(_ context.Context, env model.Environment, selection model.Selection) model.Verification {
	base, err := selectedBase(selection)
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	content, _, exists, err := adapterinternal.Read(configPath(env))
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	if !exists {
		return model.Verification{AdapterID: adapterID, Detail: "~/.condarc was not created"}
	}
	doc, err := parseDocument(content)
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	if err := verifyConfigured(doc, base); err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	if a.networkVerify {
		targets, err := a.ProbeTargets(env, selection)
		if err != nil {
			return model.Verification{AdapterID: adapterID, Detail: err.Error()}
		}
		for _, target := range targets {
			if err := adapterinternal.VerifyEndpoint(a.client, target.URL); err != nil {
				return model.Verification{AdapterID: adapterID, Detail: err.Error()}
			}
		}
	}
	return model.Verification{AdapterID: adapterID, OK: true, Detail: "Conda public channels match selected endpoint"}
}

type policy struct {
	defaults bool
	public   map[string]bool
}

func inspectPolicy(doc *yaml.Node) (policy, error) {
	result := policy{public: map[string]bool{}}
	root := documentRoot(doc)
	channels := mapValue(root, "channels")
	if channels == nil {
		result.defaults = true
	} else {
		if channels.Kind != yaml.SequenceNode {
			return result, fmt.Errorf("Conda channels must be a YAML list")
		}
		for _, item := range channels.Content {
			if item.Kind != yaml.ScalarNode {
				return result, fmt.Errorf("Conda channels must contain only strings")
			}
			name := strings.TrimSpace(item.Value)
			if name == "defaults" {
				result.defaults = true
			}
			for _, channel := range publicChannels {
				if name == channel {
					result.public[channel] = true
				}
			}
		}
	}
	if defaults := mapValue(root, "default_channels"); defaults != nil {
		if err := validateStringSequence("default_channels", defaults); err != nil {
			return result, err
		}
		if err := rejectSensitiveSequence("default_channels", defaults); err != nil {
			return result, err
		}
	}
	if custom := mapValue(root, "custom_channels"); custom != nil {
		if custom.Kind != yaml.MappingNode {
			return result, fmt.Errorf("Conda custom_channels must be a YAML mapping")
		}
		for i := 0; i+1 < len(custom.Content); i += 2 {
			key, value := custom.Content[i], custom.Content[i+1]
			if key.Kind != yaml.ScalarNode || value.Kind != yaml.ScalarNode {
				return result, fmt.Errorf("Conda custom_channels must map names to URL strings")
			}
			for _, channel := range publicChannels {
				if key.Value == channel {
					if sensitiveURL(value.Value) {
						return result, fmt.Errorf("Conda public channel %q contains credentials or variable expansion; refusing to overwrite it", channel)
					}
					result.public[channel] = true
				}
			}
		}
	}
	return result, nil
}

func configure(doc *yaml.Node, base string) error {
	current, err := inspectPolicy(doc)
	if err != nil {
		return err
	}
	root := documentRoot(doc)
	if current.defaults {
		setMapValue(root, "default_channels", stringSequence(base+"/pkgs/main", base+"/pkgs/r"))
	}
	if len(current.public) > 0 {
		custom := mapValue(root, "custom_channels")
		if custom == nil {
			custom = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMapValue(root, "custom_channels", custom)
		}
		for _, channel := range publicChannels {
			if current.public[channel] {
				setMapValue(custom, channel, scalar(base+"/cloud"))
			}
		}
	}
	return nil
}

func verifyConfigured(doc *yaml.Node, base string) error {
	current, err := inspectPolicy(doc)
	if err != nil {
		return err
	}
	root := documentRoot(doc)
	if current.defaults {
		defaults := mapValue(root, "default_channels")
		want := []string{base + "/pkgs/main", base + "/pkgs/r"}
		if !sequenceEquals(defaults, want) {
			return fmt.Errorf("Conda default_channels do not match the selected endpoint")
		}
	}
	custom := mapValue(root, "custom_channels")
	for _, channel := range publicChannels {
		if !current.public[channel] {
			continue
		}
		value := mapValue(custom, channel)
		if value == nil || strings.TrimRight(value.Value, "/") != base+"/cloud" {
			return fmt.Errorf("Conda public channel %q does not match the selected endpoint", channel)
		}
	}
	return nil
}

func parseDocument(content []byte) (*yaml.Node, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("invalid ~/.condarc YAML: %w", err)
	}
	if documentRoot(&doc).Kind != yaml.MappingNode {
		return nil, fmt.Errorf("~/.condarc must contain a YAML mapping")
	}
	if err := rejectDuplicateKeys(documentRoot(&doc)); err != nil {
		return nil, err
	}
	if _, err := inspectPolicyWithoutRecursion(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func inspectPolicyWithoutRecursion(doc *yaml.Node) (policy, error) {
	// Kept separate from parseDocument so inspectPolicy can safely be used by
	// configuration and verification without parse recursion.
	result := policy{public: map[string]bool{}}
	root := documentRoot(doc)
	channels := mapValue(root, "channels")
	if channels == nil {
		result.defaults = true
	} else if err := validateStringSequence("channels", channels); err != nil {
		return result, err
	}
	if defaults := mapValue(root, "default_channels"); defaults != nil {
		if err := validateStringSequence("default_channels", defaults); err != nil {
			return result, err
		}
	}
	if custom := mapValue(root, "custom_channels"); custom != nil && custom.Kind != yaml.MappingNode {
		return result, fmt.Errorf("Conda custom_channels must be a YAML mapping")
	}
	return result, nil
}

func encodeDocument(doc *yaml.Node) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return doc
}

func mapValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func setMapValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, scalar(key), value)
}

func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func stringSequence(values ...string) *yaml.Node {
	result := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, value := range values {
		result.Content = append(result.Content, scalar(value))
	}
	return result
}

func validateStringSequence(name string, node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("Conda %s must be a YAML list", name)
	}
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode {
			return fmt.Errorf("Conda %s must contain only strings", name)
		}
	}
	return nil
}

func rejectSensitiveSequence(name string, node *yaml.Node) error {
	for _, item := range node.Content {
		if sensitiveURL(item.Value) {
			return fmt.Errorf("Conda %s contains credentials or variable expansion; refusing to overwrite it", name)
		}
	}
	return nil
}

func sensitiveURL(value string) bool {
	if strings.Contains(value, "${") || strings.Contains(value, "$CONDA_") || strings.Contains(value, "$MAMBA_") {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.User != nil
}

func sequenceEquals(node *yaml.Node, want []string) bool {
	if node == nil || node.Kind != yaml.SequenceNode || len(node.Content) != len(want) {
		return false
	}
	for i, value := range want {
		if node.Content[i].Kind != yaml.ScalarNode || strings.TrimRight(node.Content[i].Value, "/") != value {
			return false
		}
	}
	return true
}

func hasMirrorKeys(doc *yaml.Node) bool {
	root := documentRoot(doc)
	for _, key := range []string{"channels", "default_channels", "custom_channels", "channel_alias", "custom_multichannels", "mirrored_channels"} {
		if mapValue(root, key) != nil {
			return true
		}
	}
	return false
}

func rejectDuplicateKeys(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if seen[key] {
				return fmt.Errorf("Conda configuration contains duplicate key %q", key)
			}
			seen[key] = true
			if err := rejectDuplicateKeys(node.Content[i+1]); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range node.Content {
		if err := rejectDuplicateKeys(child); err != nil {
			return err
		}
	}
	return nil
}

func platformSubdir(goos, goarch string) string {
	switch goos + "/" + goarch {
	case "darwin/arm64":
		return "osx-arm64"
	case "darwin/amd64":
		return "osx-64"
	case "linux/arm64":
		return "linux-aarch64"
	case "linux/amd64":
		return "linux-64"
	default:
		return "noarch"
	}
}
