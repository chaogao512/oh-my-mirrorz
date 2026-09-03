// Package adapterinternal contains small, deliberately conservative helpers
// shared by adapters.  It does not write files: producing Changes is the only
// way an adapter can request a configuration modification.
package adapterinternal

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

func Home(env model.Environment) string { return env.Home }

func XDGConfig(env model.Environment) string {
	if env.XDGConfigHome != "" {
		return env.XDGConfigHome
	}
	return filepath.Join(env.Home, ".config")
}

func Read(path string) ([]byte, os.FileMode, bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, 0o600, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, false, err
	}
	return b, info.Mode().Perm(), true, nil
}

func Change(adapterID, path string, scope model.Scope, after []byte) (model.Change, error) {
	before, mode, existed, err := Read(path)
	if err != nil {
		return model.Change{}, err
	}
	return model.Change{AdapterID: adapterID, Path: path, Scope: scope, Before: before, After: after, Mode: mode, Existed: existed}, nil
}

// Endpoint normalizes only syntactic details.  Resolver owns endpoint safety
// and capability checks, while this protects adapters from accidental empty
// values and embedded URL credentials.
func Endpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("endpoint must be a credential-free HTTPS URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("endpoint must not contain query or fragment")
	}
	return strings.TrimRight(u.String(), "/") + "/", nil
}

func VerifyEndpoint(client *http.Client, endpoint string) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest(http.MethodHead, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "oh-my-mirrorz/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		req, err = http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Range", "bytes=0-0")
		req.Header.Set("User-Agent", "oh-my-mirrorz/0.1")
		resp, err = client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("endpoint %s %s returned HTTP %d", resp.Request.Method, resp.Request.URL.Redacted(), resp.StatusCode)
	}
	return nil
}

func JoinLines(lines []string, finalNewline bool) []byte {
	s := strings.Join(lines, "\n")
	if finalNewline {
		s += "\n"
	}
	return []byte(s)
}

func HasFinalNewline(b []byte) bool { return bytes.HasSuffix(b, []byte("\n")) }
