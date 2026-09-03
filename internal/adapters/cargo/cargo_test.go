package cargo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

func TestPlanCreatesManagedSparseSourceAndPreservesOtherTables(t *testing.T) {
	root := t.TempDir()
	cargoHome := filepath.Join(root, "cargo")
	if err := os.MkdirAll(cargoHome, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cargoHome, "config.toml")
	before := "[net]\ngit-fetch-with-cli = true\n\n[registries.internal]\nindex = \"https://internal.example/index\"\n"
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	env := model.Environment{Home: root, CargoHome: cargoHome}
	changes, err := New().Plan(context.Background(), env, model.Selection{Endpoint: "https://mirror.example/crates.io-index"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d changes", len(changes))
	}
	got := string(changes[0].After)
	for _, want := range []string{"git-fetch-with-cli = true", "[registries.internal]", "[source.crates-io]", `replace-with = "oh-my-mirrorz"`, "[source.oh-my-mirrorz]", `registry = "sparse+https://mirror.example/crates.io-index/"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestPlanRefusesExistingCustomReplacement(t *testing.T) {
	root := t.TempDir()
	cargoHome := filepath.Join(root, "cargo")
	if err := os.MkdirAll(cargoHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cargoHome, "config"), []byte("[source.crates-io]\nreplace-with = \"company\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New().Plan(context.Background(), model.Environment{Home: root, CargoHome: cargoHome}, model.Selection{Endpoint: "https://mirror.example"})
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("want conflict error, got %v", err)
	}
}

func TestConfigWithoutExtensionTakesPrecedence(t *testing.T) {
	root := t.TempDir()
	cargoHome := filepath.Join(root, "cargo")
	if err := os.MkdirAll(cargoHome, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(cargoHome, "config")
	if err := os.WriteFile(legacy, []byte("[net]\noffline = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cargoHome, "config.toml"), []byte("[net]\noffline = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := configPath(model.Environment{Home: root, CargoHome: cargoHome}); got != legacy {
		t.Fatalf("got %q, want %q", got, legacy)
	}
}
