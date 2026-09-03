package homebrew

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

func TestPlanReplacesDuplicateManagedBlocksAndPreservesUserValues(t *testing.T) {
	root := t.TempDir()
	env := model.Environment{Home: root, XDGConfigHome: filepath.Join(root, "config")}
	p := filepath.Join(root, "config", "homebrew", "brew.env")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	before := "HOMEBREW_NO_ANALYTICS=1\n" + begin + "\nHOMEBREW_API_DOMAIN=https://old.example\n" + end + "\nHOMEBREW_COLOR=1\n" + begin + "\nHOMEBREW_API_DOMAIN=https://older.example\n" + end + "\n"
	if err := os.WriteFile(p, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	selection := model.Selection{Endpoints: map[string]string{"brew_git": "https://mirror.example/brew.git", "api": "https://mirror.example/api", "bottles": "https://mirror.example/bottles", "pypi": "https://mirror.example/pypi"}}
	changes, err := New().Plan(context.Background(), env, selection)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d changes", len(changes))
	}
	got := string(changes[0].After)
	if strings.Count(got, begin) != 1 || !strings.Contains(got, "HOMEBREW_NO_ANALYTICS=1") || !strings.Contains(got, "HOMEBREW_COLOR=1") || !strings.Contains(got, "HOMEBREW_BOTTLE_DOMAIN=https://mirror.example/bottles") {
		t.Fatalf("unexpected environment config:\n%s", got)
	}
	if strings.Contains(got, "='https://") {
		t.Fatalf("brew.env values must be literal and unquoted:\n%s", got)
	}
	if strings.Contains(got, "HOMEBREW_BREW_GIT_REMOTE") {
		t.Fatalf("Git remote must remain untouched by default:\n%s", got)
	}
}

func TestBrewGitRemoteRequiresExplicitAdapterOptIn(t *testing.T) {
	root := t.TempDir()
	env := model.Environment{Home: root, XDGConfigHome: filepath.Join(root, "config")}
	config := filepath.Join(env.XDGConfigHome, "homebrew", "brew.env")
	if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	selection := model.Selection{Endpoints: map[string]string{"brew_git": "https://mirror.example/brew.git", "api": "https://mirror.example/api", "bottles": "https://mirror.example/bottles", "pypi": "https://mirror.example/pypi"}}
	changes, err := New(WithBrewGitRemote(true)).Plan(context.Background(), env, selection)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !strings.Contains(string(changes[0].After), "HOMEBREW_BREW_GIT_REMOTE=https://mirror.example/brew.git") {
		t.Fatalf("explicit Git remote opt-in was not honored: %#v", changes)
	}
}

func TestConfigPathPrefersXDGEnvironmentFile(t *testing.T) {
	root := t.TempDir()
	env := model.Environment{Home: root, XDGConfigHome: filepath.Join(root, "config")}
	modern := filepath.Join(root, "config", "homebrew", "brew.env")
	legacy := filepath.Join(root, ".homebrew", "brew.env")
	if err := os.MkdirAll(filepath.Dir(modern), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modern, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("A=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := configPath(env); got != modern {
		t.Fatalf("got %q, want %q", got, modern)
	}
}
