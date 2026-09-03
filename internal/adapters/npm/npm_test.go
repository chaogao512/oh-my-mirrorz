package npm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

func TestPlanChangesOnlyUnscopedRegistry(t *testing.T) {
	root := t.TempDir()
	env := model.Environment{Home: root}
	before := "//registry.npmjs.org/:_authToken=secret-value\n@private:registry=https://private.example/\nregistry=https://registry.npmjs.org/\nalways-auth=true\n"
	if err := os.WriteFile(filepath.Join(root, ".npmrc"), []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	changes, err := New().Plan(context.Background(), env, model.Selection{Endpoint: "https://registry.npmmirror.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d changes", len(changes))
	}
	got := string(changes[0].After)
	if !strings.Contains(got, "//registry.npmjs.org/:_authToken=secret-value") || !strings.Contains(got, "@private:registry=https://private.example/") {
		t.Fatalf("sensitive or scoped setting changed:\n%s", got)
	}
	if !strings.Contains(got, "registry=https://registry.npmmirror.com/") {
		t.Fatalf("registry was not replaced:\n%s", got)
	}
	if verify := New().Verify(context.Background(), env, model.Selection{Endpoint: "https://registry.npmmirror.com"}); verify.OK {
		t.Fatal("Verify must read the current file, not an unapplied plan")
	}
}

func TestSetRegistryAddsValueWithoutDestroyingComments(t *testing.T) {
	got := string(setRegistry([]byte("# token is configured elsewhere\n@scope:registry=https://private.example/\n"), "https://registry.npmmirror.com/"))
	if !strings.Contains(got, "# token is configured elsewhere") || !strings.Contains(got, "@scope:registry=https://private.example/") || !strings.Contains(got, "registry=https://registry.npmmirror.com/") {
		t.Fatalf("unexpected output:\n%s", got)
	}
}

func TestDetectRejectsEnvironmentOverride(t *testing.T) {
	got := New().Detect(context.Background(), model.Environment{Home: t.TempDir(), NPMConfigOverride: true})
	if got.Status != model.StatusInvalidConfig {
		t.Fatalf("unexpected detection: %#v", got)
	}
}
