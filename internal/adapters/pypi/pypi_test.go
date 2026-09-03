package pypi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

func TestPlanPreservesPipAndUVConfiguration(t *testing.T) {
	root := t.TempDir()
	env := model.Environment{Home: root, XDGConfigHome: filepath.Join(root, "config")}
	pip, uv := paths(env)
	if err := os.MkdirAll(filepath.Dir(pip), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(uv), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pip, []byte("[global]\ntimeout = 30\nindex-url = https://old.example/simple\n[install]\nuser = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(uv, []byte("cache-dir = \"/tmp/cache\"\n[[index]]\nname = \"old\"\nurl = \"https://old.example/simple\"\ndefault = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New()
	changes, err := a.Plan(context.Background(), env, model.Selection{AdapterID: adapterID, Endpoint: "https://mirror.example/pypi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("got %d changes", len(changes))
	}
	gotPip, gotUV := string(changes[0].After), string(changes[1].After)
	if !strings.Contains(gotPip, "timeout = 30") || !strings.Contains(gotPip, "index-url = https://mirror.example/pypi/") || !strings.Contains(gotPip, "[install]") {
		t.Fatalf("pip config not preserved:\n%s", gotPip)
	}
	if !strings.Contains(gotUV, "cache-dir") || !strings.Contains(gotUV, `url = "https://mirror.example/pypi/"`) || !strings.Contains(gotUV, "default = true") {
		t.Fatalf("uv config not preserved:\n%s", gotUV)
	}
}

func TestVerifyChecksUserConfiguration(t *testing.T) {
	root := t.TempDir()
	env := model.Environment{Home: root, XDGConfigHome: filepath.Join(root, "config")}
	pip, _ := paths(env)
	if err := os.MkdirAll(filepath.Dir(pip), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pip, []byte("[global]\nindex-url = https://mirror.example/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := New().Verify(context.Background(), env, model.Selection{Endpoint: "https://mirror.example"}); !got.OK {
		t.Fatal(got.Detail)
	}
}

func TestMacOSUsesApplicationSupportAndExistingLegacyConfig(t *testing.T) {
	root := t.TempDir()
	env := model.Environment{Home: root, XDGConfigHome: filepath.Join(root, "config"), GOOS: "darwin"}
	preferred, _ := paths(env)
	legacy := filepath.Join(root, ".pip", "pip.conf")
	for _, path := range []string{preferred, legacy} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("[global]\ntimeout = 10\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	changes, err := New().Plan(context.Background(), env, model.Selection{Endpoint: "https://mirror.example/simple"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, change := range changes {
		seen[change.Path] = true
	}
	if !seen[preferred] || !seen[legacy] {
		t.Fatalf("macOS pip configs not covered: %#v", seen)
	}
}

func TestDetectRejectsEnvironmentOverride(t *testing.T) {
	env := model.Environment{Home: t.TempDir(), PipIndexOverride: true}
	if got := New().Detect(context.Background(), env); got.Status != model.StatusInvalidConfig {
		t.Fatalf("unexpected detection: %#v", got)
	}
}
