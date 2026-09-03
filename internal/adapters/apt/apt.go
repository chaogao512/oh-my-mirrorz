// Package apt handles Debian and Ubuntu source files.  It produces only
// system-scoped Changes; the transaction layer remains responsible for any
// privileged write.
package apt

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	adapterinternal "github.com/chaogao512/oh-my-mirrorz/internal/adapters/internal"
	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

const adapterID = "apt"

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

func root(env model.Environment) string {
	if env.SystemRoot != "" {
		return env.SystemRoot
	}
	return "/"
}
func osRelease(env model.Environment) (string, error) {
	b, err := os.ReadFile(filepath.Join(root(env), "etc", "os-release"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && k == "ID" {
			return strings.Trim(v, `"`), nil
		}
	}
	return "", fmt.Errorf("os-release has no ID")
}
func sourcePaths(env model.Environment) ([]string, error) {
	base := filepath.Join(root(env), "etc", "apt")
	paths := []string{}
	for _, name := range []string{"sources.list", "sources.list.d"} {
		p := filepath.Join(base, name)
		info, err := os.Stat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			paths = append(paths, p)
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() && (strings.HasSuffix(entry.Name(), ".list") || strings.HasSuffix(entry.Name(), ".sources")) {
				paths = append(paths, filepath.Join(p, entry.Name()))
			}
		}
	}
	return paths, nil
}

func (a *Adapter) Detect(_ context.Context, env model.Environment) model.Detection {
	id, err := osRelease(env)
	if err != nil {
		return model.Detection{AdapterID: adapterID, Status: model.StatusNotInstalled, Scope: model.ScopeSystem, Reason: "APT os-release not found"}
	}
	if id != "debian" && id != "ubuntu" {
		return model.Detection{AdapterID: adapterID, Status: model.StatusUnsupported, Scope: model.ScopeSystem, Reason: "only Debian and Ubuntu are supported"}
	}
	paths, err := sourcePaths(env)
	if err != nil {
		return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeSystem, Reason: err.Error()}
	}
	if len(paths) == 0 {
		return model.Detection{AdapterID: adapterID, Status: model.StatusNotInstalled, Scope: model.ScopeSystem, Reason: "no APT source files found"}
	}
	for _, path := range paths {
		b, _, _, readErr := adapterinternal.Read(path)
		if readErr != nil || validate(path, b) != nil {
			reason := "invalid APT source file"
			if readErr != nil {
				reason = readErr.Error()
			}
			return model.Detection{AdapterID: adapterID, Status: model.StatusInvalidConfig, Scope: model.ScopeSystem, ConfigPaths: paths, Reason: reason}
		}
	}
	return model.Detection{AdapterID: adapterID, Status: model.StatusDetected, Scope: model.ScopeSystem, ConfigPaths: paths}
}
func (a *Adapter) Inspect(_ context.Context, env model.Environment) ([]byte, error) {
	paths, err := sourcePaths(env)
	if err != nil {
		return nil, err
	}
	var parts []string
	for _, path := range paths {
		b, _, _, err := adapterinternal.Read(path)
		if err != nil {
			return nil, err
		}
		parts = append(parts, "# "+path+"\n"+string(b))
	}
	return []byte(strings.Join(parts, "\n")), nil
}

func aptEndpoint(selection model.Selection, distro string, port bool) (string, error) {
	keys := []string{distro, "apt", "apt-main"}
	if distro == "ubuntu" && port {
		keys = []string{"ubuntu-ports", "apt-ports", "apt_ports", "ubuntu", "apt"}
	}
	v := selection.Endpoint
	for _, key := range keys {
		if selection.Endpoints != nil && selection.Endpoints[key] != "" {
			v = selection.Endpoints[key]
			break
		}
	}
	prefix := ""
	if strings.HasPrefix(v, "mirror+") {
		prefix, v = "mirror+", strings.TrimPrefix(v, "mirror+")
	} else if selection.Strategy == model.StrategyAuto {
		// APT's mirror+ transport consumes MirrorZ's ordered mirrorlist and
		// keeps client-side failover, rather than pinning auto selection to the
		// one redirect target observed while resolving.
		prefix = "mirror+"
	}
	normalized, err := adapterinternal.Endpoint(v)
	if err != nil {
		return "", err
	}
	return prefix + strings.TrimRight(normalized, "/"), nil
}

func aptSecurityEndpoint(selection model.Selection, distro string) (string, error) {
	if distro != "debian" {
		return aptEndpoint(selection, distro, false)
	}
	copy := selection
	copy.Endpoint = ""
	copy.Endpoints = map[string]string{}
	for _, key := range []string{"debian-security", "apt-security", "apt_security"} {
		if selection.Endpoints[key] != "" {
			copy.Endpoint = selection.Endpoints[key]
			break
		}
	}
	if copy.Endpoint == "" {
		return "", fmt.Errorf("selection has no Debian security endpoint")
	}
	return aptEndpoint(copy, distro, false)
}

func (a *Adapter) Plan(_ context.Context, env model.Environment, selection model.Selection) ([]model.Change, error) {
	if selection.AdapterID != "" && selection.AdapterID != adapterID {
		return nil, fmt.Errorf("selection is for %q, not %q", selection.AdapterID, adapterID)
	}
	if !env.IncludeSystem {
		return nil, nil
	}
	d := a.Detect(context.Background(), env)
	if d.Status != model.StatusDetected {
		return nil, fmt.Errorf("APT cannot be planned: %s", d.Reason)
	}
	distro, err := osRelease(env)
	if err != nil {
		return nil, err
	}
	main, err := aptEndpoint(selection, distro, false)
	if err != nil {
		return nil, err
	}
	ports, err := aptEndpoint(selection, distro, true)
	if err != nil {
		return nil, err
	}
	security, err := aptSecurityEndpoint(selection, distro)
	if err != nil && env.IncludeSecurity {
		return nil, err
	}
	changes := make([]model.Change, 0, len(d.ConfigPaths))
	for _, path := range d.ConfigPaths {
		before, _, _, err := adapterinternal.Read(path)
		if err != nil {
			return nil, err
		}
		var after []byte
		if strings.HasSuffix(path, ".sources") {
			after, err = rewriteDEB822(before, distro, main, ports, security, env.IncludeSecurity)
		} else {
			after, err = rewriteList(before, distro, main, ports, security, env.IncludeSecurity)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		change, err := adapterinternal.Change(adapterID, path, model.ScopeSystem, after)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func validate(path string, b []byte) error {
	if strings.HasSuffix(path, ".sources") {
		_, err := rewriteDEB822(b, "ubuntu", "https://invalid.example", "https://invalid.example", "https://invalid.example", true)
		return err
	}
	_, err := rewriteList(b, "ubuntu", "https://invalid.example", "https://invalid.example", "https://invalid.example", true)
	return err
}
func isSecurity(text string) bool {
	t := strings.ToLower(text)
	return strings.Contains(t, "security.ubuntu.com") || strings.Contains(t, "-security") || strings.Contains(t, "/security")
}
func isPorts(uri string) bool {
	return strings.Contains(strings.ToLower(uri), "ports.ubuntu.com") || strings.Contains(strings.ToLower(uri), "ubuntu-ports")
}

func isOfficialDistroURI(raw, distro string) bool {
	raw = strings.TrimPrefix(raw, "mirror+")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.Trim(strings.ToLower(u.Path), "/")
	switch distro {
	case "ubuntu":
		return host == "archive.ubuntu.com" || host == "security.ubuntu.com" ||
			host == "ports.ubuntu.com" || host == "old-releases.ubuntu.com" ||
			path == "ubuntu" || path == "ubuntu-ports" ||
			(host == "mirrors.cernet.edu.cn" && (path == "api/apt/mirrorlist/ubuntu" || path == "api/apt/mirrorlist/ubuntu-ports"))
	case "debian":
		return host == "deb.debian.org" || host == "security.debian.org" ||
			host == "ftp.debian.org" || path == "debian" || path == "debian-security" ||
			(host == "mirrors.cernet.edu.cn" && path == "api/apt/mirrorlist/debian")
	default:
		return false
	}
}

func rewriteList(before []byte, distro, main, ports, security string, includeSecurity bool) ([]byte, error) {
	lines := strings.Split(strings.TrimSuffix(string(before), "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 || (fields[0] != "deb" && fields[0] != "deb-src") {
			continue
		}
		uri := -1
		for j := 1; j < len(fields); j++ {
			if strings.Contains(fields[j], "://") || strings.HasPrefix(fields[j], "mirror+") {
				uri = j
				break
			}
		}
		if uri < 0 || uri+1 >= len(fields) {
			return nil, fmt.Errorf("malformed APT source line")
		}
		if !isOfficialDistroURI(fields[uri], distro) {
			continue
		}
		if !includeSecurity && isSecurity(trimmed) {
			continue
		}
		target := main
		if isSecurity(trimmed) {
			target = security
		} else if isPorts(fields[uri]) {
			target = ports
		}
		// Retain original indentation only.  Comment preservation on active deb
		// lines is intentionally conservative: malformed trailing comments are
		// left as ordinary fields rather than discarded.
		prefix := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		fields[uri] = target
		lines[i] = prefix + strings.Join(fields, " ")
	}
	return adapterinternal.JoinLines(lines, adapterinternal.HasFinalNewline(before)), nil
}

func rewriteDEB822(before []byte, distro, main, ports, security string, includeSecurity bool) ([]byte, error) {
	stanzas := strings.Split(string(before), "\n\n")
	for i, stanza := range stanzas {
		if strings.TrimSpace(stanza) == "" {
			continue
		}
		lines := strings.Split(stanza, "\n")
		active, uris := false, -1
		for j, line := range lines {
			k, _, ok := strings.Cut(line, ":")
			if !ok {
				if strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "#") {
					return nil, fmt.Errorf("malformed DEB822 field")
				}
				continue
			}
			k = strings.TrimSpace(k)
			if k == "Types" {
				active = strings.Contains(line, "deb")
			}
			if k == "URIs" {
				uris = j
			}
		}
		if !active {
			continue
		}
		if uris < 0 {
			return nil, fmt.Errorf("DEB822 deb stanza has no URIs field")
		}
		if !includeSecurity && isSecurity(stanza) {
			continue
		}
		old := strings.TrimSpace(strings.TrimPrefix(lines[uris], "URIs:"))
		if old == "" {
			return nil, fmt.Errorf("empty DEB822 URIs")
		}
		official := true
		for _, uri := range strings.Fields(old) {
			if !isOfficialDistroURI(uri, distro) {
				official = false
				break
			}
		}
		if !official {
			continue
		}
		target := main
		if isSecurity(stanza) {
			target = security
		} else if isPorts(old) {
			target = ports
		}
		lines[uris] = "URIs: " + target
		stanzas[i] = strings.Join(lines, "\n")
	}
	return []byte(strings.Join(stanzas, "\n\n")), nil
}

func (a *Adapter) Verify(_ context.Context, env model.Environment, selection model.Selection) model.Verification {
	if !env.IncludeSystem {
		return model.Verification{AdapterID: adapterID, OK: true, Detail: "APT excluded without --system"}
	}
	distro, err := osRelease(env)
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	main, err := aptEndpoint(selection, distro, false)
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	ports, err := aptEndpoint(selection, distro, true)
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	security, err := aptSecurityEndpoint(selection, distro)
	if err != nil && env.IncludeSecurity {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	paths, err := sourcePaths(env)
	if err != nil {
		return model.Verification{AdapterID: adapterID, Detail: err.Error()}
	}
	for _, path := range paths {
		b, _, _, err := adapterinternal.Read(path)
		if err != nil {
			return model.Verification{AdapterID: adapterID, Detail: err.Error()}
		}
		rewritten, err := func() ([]byte, error) {
			if strings.HasSuffix(path, ".sources") {
				return rewriteDEB822(b, distro, main, ports, security, env.IncludeSecurity)
			}
			return rewriteList(b, distro, main, ports, security, env.IncludeSecurity)
		}()
		if err != nil {
			return model.Verification{AdapterID: adapterID, Detail: err.Error()}
		}
		if string(rewritten) != string(b) {
			return model.Verification{AdapterID: adapterID, Detail: "APT sources do not match selected endpoint"}
		}
	}
	if a.networkVerify {
		targets := []string{main, ports}
		if env.IncludeSecurity {
			targets = append(targets, security)
		}
		seen := map[string]bool{}
		for _, target := range targets {
			target = strings.TrimPrefix(target, "mirror+")
			if target == "" || seen[target] {
				continue
			}
			seen[target] = true
			if err := adapterinternal.VerifyEndpoint(a.client, strings.TrimRight(target, "/")+"/"); err != nil {
				return model.Verification{AdapterID: adapterID, Detail: err.Error()}
			}
		}
	}
	return model.Verification{AdapterID: adapterID, OK: true, Detail: "APT source configuration matches selected endpoint"}
}
