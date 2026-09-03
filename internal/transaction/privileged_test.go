package transaction

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

type recordingRunner struct{ calls [][]string }

func (r *recordingRunner) Run(name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil
}

func TestPrivilegedWriterUsesSudoOnlyForSystemScope(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.conf")
	if err := os.WriteFile(userPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	w := PrivilegedWriter{Runner: runner, SystemRoot: dir}
	if err := w.Apply(model.Change{AdapterID: "test", Path: userPath, Scope: model.ScopeUser, Before: []byte("old"), After: []byte("new"), Mode: 0o600, Existed: true}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("user write invoked sudo: %v", runner.calls)
	}
	if got, _ := os.ReadFile(userPath); string(got) != "new" {
		t.Fatalf("got %q", got)
	}

	systemPath := filepath.Join(dir, "etc", "apt", "sources.list")
	if err := os.MkdirAll(filepath.Dir(systemPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(systemPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Apply(model.Change{AdapterID: "apt", Path: systemPath, Scope: model.ScopeSystem, Before: []byte("old"), After: []byte("new"), Mode: 0o644, Existed: true}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 || !reflect.DeepEqual(runner.calls[0][:4], []string{"sudo", "install", "-m", "644"}) || !reflect.DeepEqual(runner.calls[1][:3], []string{"sudo", "mv", "-f"}) {
		t.Fatalf("unexpected sudo calls: %v", runner.calls)
	}
}

func TestPrivilegedWriterRejectsNonAPTSystemPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "etc", "passwd")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	err := (PrivilegedWriter{Runner: runner, SystemRoot: dir}).Apply(model.Change{AdapterID: "apt", Path: path, Scope: model.ScopeSystem, Before: []byte("old"), After: []byte("new"), Mode: 0o644, Existed: true})
	if err == nil || len(runner.calls) != 0 {
		t.Fatalf("unsafe path was not rejected: err=%v calls=%v", err, runner.calls)
	}
}
