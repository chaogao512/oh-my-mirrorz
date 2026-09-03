package runtimeenv

import (
	"path/filepath"
	"testing"
)

func TestFromHomeUsesXDGDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("CARGO_HOME", "")
	t.Setenv("HOMEBREW_XDG_CONFIG_HOME", "")
	home := filepath.Join(string(filepath.Separator), "tmp", "alice")
	env := FromHome(home, true, false)
	if env.XDGConfigHome != filepath.Join(home, ".config") {
		t.Fatalf("unexpected config home: %s", env.XDGConfigHome)
	}
	if env.CargoHome != filepath.Join(home, ".cargo") || !env.IncludeSystem {
		t.Fatalf("unexpected environment: %#v", env)
	}
	if env.HomebrewConfigHome != filepath.Join(home, ".homebrew") {
		t.Fatalf("unexpected Homebrew config home: %s", env.HomebrewConfigHome)
	}
}

func TestFromHomeHonorsHomebrewXDGPrecedence(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "tmp", "alice")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOMEBREW_XDG_CONFIG_HOME", filepath.Join(home, "brew-xdg"))
	env := FromHome(home, false, false)
	if env.HomebrewConfigHome != filepath.Join(home, "brew-xdg", "homebrew") {
		t.Fatalf("unexpected Homebrew config home: %s", env.HomebrewConfigHome)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	env = FromHome(home, false, false)
	if env.HomebrewConfigHome != filepath.Join(home, "xdg", "homebrew") {
		t.Fatalf("XDG_CONFIG_HOME did not take priority: %s", env.HomebrewConfigHome)
	}
}
