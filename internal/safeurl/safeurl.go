package safeurl

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
)

func Normalize(raw string) string {
	return strings.TrimPrefix(raw, "sparse+")
}

func Validate(raw string, allowPrivate bool) error {
	normalized := Normalize(raw)
	u, err := url.Parse(normalized)
	if err != nil {
		return fmt.Errorf("parse endpoint: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("endpoint must use https")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("endpoint has no host")
	}
	if u.User != nil {
		return fmt.Errorf("endpoint must not contain credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("endpoint must not contain query or fragment")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if !allowPrivate && (host == "localhost" || strings.HasSuffix(host, ".localhost")) {
		return fmt.Errorf("private endpoint requires explicit opt-in")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !allowPrivate && isPrivate(ip) {
		return fmt.Errorf("private endpoint requires explicit opt-in")
	}
	return nil
}

// DialContext resolves the destination before connecting and pins the
// connection to a validated address. This closes the DNS-rebinding gap left by
// validating only a URL's textual hostname.
func DialContext(allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		// Prefer IPv4 when both families are available. Mirror endpoints in
		// restricted networks often publish IPv6 even when the client has no
		// working IPv6 route; trying it first would consume the whole timeout.
		sort.SliceStable(addresses, func(i, j int) bool {
			return addresses[i].IP.To4() != nil && addresses[j].IP.To4() == nil
		})
		var lastErr error
		dialer := &net.Dialer{}
		for _, candidate := range addresses {
			if !allowPrivate && isPrivate(candidate.IP) {
				lastErr = fmt.Errorf("resolved private endpoint requires explicit opt-in")
				continue
			}
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("endpoint resolved to no usable address")
		}
		return nil, lastErr
	}
}

func isPrivate(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified()
}

func Redact(raw string) string {
	normalized := Normalize(raw)
	u, err := url.Parse(normalized)
	if err != nil {
		return "<invalid-url>"
	}
	if u.User != nil {
		u.User = url.User("REDACTED")
	}
	q := u.Query()
	for key := range q {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "key") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
			q.Set(key, "REDACTED")
		}
	}
	u.RawQuery = q.Encode()
	result := u.String()
	if strings.HasPrefix(raw, "sparse+") {
		return "sparse+" + result
	}
	return result
}
