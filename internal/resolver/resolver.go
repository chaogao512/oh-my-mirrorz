package resolver

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
	"github.com/chaogao512/oh-my-mirrorz/internal/safeurl"
)

type Provider struct {
	AdapterID string
	Mirror    string
	Endpoint  string
	Endpoints map[string]string
	Reason    string
}

type Catalog struct {
	providers []Provider
}

func BuiltInCatalog() *Catalog {
	return &Catalog{providers: []Provider{
		{AdapterID: "pypi", Mirror: "auto", Endpoint: "https://mirrors.cernet.edu.cn/pypi/web/simple", Reason: "MirrorZ repository-aware redirect"},
		{AdapterID: "npm", Mirror: "auto", Endpoint: "https://registry.npmmirror.com/", Reason: "registered npm read-only mirror"},
		{AdapterID: "cargo", Mirror: "auto", Endpoint: "sparse+https://mirrors.cernet.edu.cn/crates.io-index/", Reason: "MirrorZ crates.io sparse index redirect"},
		{AdapterID: "homebrew", Mirror: "auto", Endpoints: map[string]string{
			"brew_git": "https://mirrors.cernet.edu.cn/brew.git",
			"api":      "https://mirrors.cernet.edu.cn/homebrew-bottles/api",
			"bottles":  "https://mirrors.cernet.edu.cn/homebrew-bottles",
			"pypi":     "https://mirrors.cernet.edu.cn/pypi/web/simple",
		}, Reason: "MirrorZ repository-aware redirects"},
		{AdapterID: "apt", Mirror: "auto", Endpoints: map[string]string{
			"ubuntu":          "https://mirrors.cernet.edu.cn/api/apt/mirrorlist/ubuntu",
			"ubuntu-ports":    "https://mirrors.cernet.edu.cn/api/apt/mirrorlist/ubuntu-ports",
			"debian":          "https://mirrors.cernet.edu.cn/api/apt/mirrorlist/debian",
			"debian-security": "https://mirrors.cernet.edu.cn/api/apt/mirrorlist/debian-security",
		}, Reason: "MirrorZ ordered APT mirrorlist"},
		{AdapterID: "pypi", Mirror: "tuna", Endpoint: "https://pypi.tuna.tsinghua.edu.cn/simple", Reason: "fixed TUNA endpoint"},
		{AdapterID: "pypi", Mirror: "ustc", Endpoint: "https://mirrors.ustc.edu.cn/pypi/web/simple", Reason: "fixed USTC endpoint"},
		{AdapterID: "npm", Mirror: "npmmirror", Endpoint: "https://registry.npmmirror.com/", Reason: "fixed npmmirror endpoint"},
		{AdapterID: "npm", Mirror: "upstream", Endpoint: "https://registry.npmjs.org/", Reason: "official npm registry"},
		{AdapterID: "cargo", Mirror: "tuna", Endpoint: "sparse+https://mirrors.tuna.tsinghua.edu.cn/crates.io-index/", Reason: "fixed TUNA endpoint"},
		{AdapterID: "cargo", Mirror: "ustc", Endpoint: "sparse+https://mirrors.ustc.edu.cn/crates.io-index/", Reason: "fixed USTC endpoint"},
		{AdapterID: "homebrew", Mirror: "tuna", Endpoints: map[string]string{
			"brew_git": "https://mirrors.tuna.tsinghua.edu.cn/git/homebrew/brew.git",
			"api":      "https://mirrors.tuna.tsinghua.edu.cn/homebrew-bottles/api",
			"bottles":  "https://mirrors.tuna.tsinghua.edu.cn/homebrew-bottles",
			"pypi":     "https://pypi.tuna.tsinghua.edu.cn/simple",
		}, Reason: "fixed TUNA endpoints"},
		{AdapterID: "homebrew", Mirror: "ustc", Endpoints: map[string]string{
			"brew_git": "https://mirrors.ustc.edu.cn/brew.git",
			"api":      "https://mirrors.ustc.edu.cn/homebrew-bottles/api",
			"bottles":  "https://mirrors.ustc.edu.cn/homebrew-bottles",
			"pypi":     "https://mirrors.ustc.edu.cn/pypi/web/simple",
		}, Reason: "fixed USTC endpoints"},
		{AdapterID: "apt", Mirror: "tuna", Endpoints: map[string]string{
			"ubuntu":          "https://mirrors.tuna.tsinghua.edu.cn/ubuntu/",
			"ubuntu-ports":    "https://mirrors.tuna.tsinghua.edu.cn/ubuntu-ports/",
			"debian":          "https://mirrors.tuna.tsinghua.edu.cn/debian/",
			"debian-security": "https://mirrors.tuna.tsinghua.edu.cn/debian-security/",
		}, Reason: "fixed TUNA endpoints"},
		{AdapterID: "apt", Mirror: "ustc", Endpoints: map[string]string{
			"ubuntu":          "https://mirrors.ustc.edu.cn/ubuntu/",
			"ubuntu-ports":    "https://mirrors.ustc.edu.cn/ubuntu-ports/",
			"debian":          "https://mirrors.ustc.edu.cn/debian/",
			"debian-security": "https://mirrors.ustc.edu.cn/debian-security/",
		}, Reason: "fixed USTC endpoints"},
	}}
}

func (c *Catalog) Mirrors(adapterID string) []string {
	seen := map[string]bool{}
	var result []string
	for _, p := range c.providers {
		if p.AdapterID == adapterID && !seen[p.Mirror] {
			seen[p.Mirror] = true
			result = append(result, p.Mirror)
		}
	}
	sort.Strings(result)
	return result
}

type Resolver struct {
	Catalog      *Catalog
	Clock        func() time.Time
	AllowPrivate bool
}

func New() *Resolver {
	return &Resolver{Catalog: BuiltInCatalog(), Clock: time.Now}
}

func (r *Resolver) Resolve(adapterID string, strategy model.Strategy, mirror string) (model.Selection, error) {
	if r.Catalog == nil {
		return model.Selection{}, fmt.Errorf("resolver catalog is nil")
	}
	var provider Provider
	var ok bool
	switch strategy {
	case model.StrategyAuto:
		provider, ok = r.lookup(adapterID, "auto")
	case model.StrategyFixed:
		if mirror == "" {
			return model.Selection{}, fmt.Errorf("fixed strategy requires a mirror")
		}
		provider, ok = r.lookup(adapterID, strings.ToLower(mirror))
	case model.StrategyPrefer:
		if mirror == "" {
			return model.Selection{}, fmt.Errorf("prefer strategy requires a mirror")
		}
		provider, ok = r.lookup(adapterID, strings.ToLower(mirror))
		if !ok {
			provider, ok = r.lookup(adapterID, "auto")
			if ok {
				provider.Reason = fmt.Sprintf("preferred mirror %q unavailable; %s", mirror, provider.Reason)
			}
		}
	default:
		return model.Selection{}, fmt.Errorf("unsupported strategy %q", strategy)
	}
	if !ok {
		return model.Selection{}, fmt.Errorf("no %s endpoint for mirror %q", adapterID, mirror)
	}
	if err := validateProvider(provider, r.AllowPrivate); err != nil {
		return model.Selection{}, err
	}
	clock := r.Clock
	if clock == nil {
		clock = time.Now
	}
	return model.Selection{
		AdapterID:  adapterID,
		Provider:   provider.Mirror,
		Mirror:     provider.Mirror,
		Endpoint:   provider.Endpoint,
		Endpoints:  cloneMap(provider.Endpoints),
		Strategy:   strategy,
		Reason:     provider.Reason,
		ResolvedAt: clock().UTC(),
	}, nil
}

func (r *Resolver) lookup(adapterID, mirror string) (Provider, bool) {
	for _, provider := range r.Catalog.providers {
		if provider.AdapterID == adapterID && provider.Mirror == mirror {
			return provider, true
		}
	}
	return Provider{}, false
}

func validateProvider(provider Provider, allowPrivate bool) error {
	if provider.Endpoint != "" {
		if err := safeurl.Validate(provider.Endpoint, allowPrivate); err != nil {
			return fmt.Errorf("invalid %s endpoint: %w", provider.AdapterID, err)
		}
	}
	for name, endpoint := range provider.Endpoints {
		if err := safeurl.Validate(endpoint, allowPrivate); err != nil {
			return fmt.Errorf("invalid %s endpoint %s: %w", provider.AdapterID, name, err)
		}
	}
	return nil
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

type ProbeResult struct {
	Endpoint string
	Status   int
	Latency  time.Duration
	FinalURL string
}

type Prober interface {
	Probe(context.Context, string) (ProbeResult, error)
}

type HTTPProber struct {
	Client *http.Client
}

func (p HTTPProber) Probe(ctx context.Context, endpoint string) (ProbeResult, error) {
	if err := safeurl.Validate(endpoint, false); err != nil {
		return ProbeResult{}, err
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	urlValue := safeurl.Normalize(endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, urlValue, nil)
	if err != nil {
		return ProbeResult{}, err
	}
	req.Header.Set("User-Agent", "oh-my-mirrorz/0.1")
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return ProbeResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, urlValue, nil)
		if err != nil {
			return ProbeResult{}, err
		}
		req.Header.Set("Range", "bytes=0-0")
		req.Header.Set("User-Agent", "oh-my-mirrorz/0.1")
		resp, err = client.Do(req)
		if err != nil {
			return ProbeResult{}, err
		}
		defer resp.Body.Close()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return ProbeResult{}, fmt.Errorf("probe %s %s returned HTTP %d", resp.Request.Method, safeurl.Redact(resp.Request.URL.String()), resp.StatusCode)
	}
	return ProbeResult{Endpoint: endpoint, Status: resp.StatusCode, Latency: time.Since(start), FinalURL: resp.Request.URL.String()}, nil
}
