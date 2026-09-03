package transaction

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
	"github.com/chaogao512/oh-my-mirrorz/internal/state"
)

func TestVerifyFailureRestoresBytesAndMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "home", ".npmrc")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("registry=old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Store: state.New(filepath.Join(root, "state"))}
	change := model.Change{AdapterID: "npm", Path: path, Scope: model.ScopeUser, Before: []byte("registry=old\n"), After: []byte("registry=new\n"), Mode: 0o640, Existed: true}
	result := engine.Run(context.Background(), []model.Change{change}, func(context.Context) error { return errors.New("network failed") })
	if result.Err == nil || result.Manifest.Status != state.StatusRolledBack {
		t.Fatalf("unexpected result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "registry=old\n" {
		t.Fatalf("restored data = %q, %v", data, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("restored mode = %o", info.Mode().Perm())
	}
}

type failingRestoreWriter struct{ FileWriter }

func (failingRestoreWriter) Restore(state.Entry, []byte) error { return errors.New("denied") }

func TestRollbackFailureIsDegraded(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Store: state.New(filepath.Join(root, "state")), Writer: failingRestoreWriter{}}
	change := model.Change{AdapterID: "pip", Path: path, Scope: model.ScopeUser, Before: []byte("before"), After: []byte("after"), Mode: 0o600, Existed: true}
	result := engine.Run(context.Background(), []model.Change{change}, func(context.Context) error { return errors.New("fail") })
	if result.Err == nil || result.Manifest.Status != state.StatusDegraded {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRestoreCreatesUndoableRecoverySnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Store: state.New(filepath.Join(root, "state"))}
	change := model.Change{AdapterID: "pip", Path: path, Scope: model.ScopeUser, Before: []byte("before"), After: []byte("after"), Mode: 0o600, Existed: true}
	applied := engine.Run(context.Background(), []model.Change{change}, nil)
	if applied.Err != nil {
		t.Fatal(applied.Err)
	}
	restored := engine.Restore(applied.Manifest.ID)
	if restored.Err != nil || restored.Manifest.Kind != "restore" || restored.Manifest.TargetID != applied.Manifest.ID {
		t.Fatalf("unexpected restore: %#v", restored)
	}
	if data, _ := os.ReadFile(path); string(data) != "before" {
		t.Fatalf("after restore = %q", data)
	}
	undone := engine.Restore(restored.Manifest.ID)
	if undone.Err != nil {
		t.Fatal(undone.Err)
	}
	if data, _ := os.ReadFile(path); string(data) != "after" {
		t.Fatalf("after undo restore = %q", data)
	}
}

func TestRepeatedRestoreIsNoOp(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Store: state.New(filepath.Join(root, "state"))}
	change := model.Change{AdapterID: "pip", Path: path, Scope: model.ScopeUser, Before: []byte("before"), After: []byte("after"), Mode: 0o600, Existed: true}
	applied := engine.Run(context.Background(), []model.Change{change}, nil)
	if applied.Err != nil {
		t.Fatal(applied.Err)
	}
	first := engine.Restore(applied.Manifest.ID)
	if first.Err != nil {
		t.Fatal(first.Err)
	}
	second := engine.Restore(applied.Manifest.ID)
	if second.Err != nil || !second.NoOp || second.Manifest.ID != applied.Manifest.ID {
		t.Fatalf("unexpected repeated restore: %#v", second)
	}
}

type failSecondApplyWriter struct {
	FileWriter
	count int
}

func (w *failSecondApplyWriter) Apply(change model.Change) error {
	w.count++
	if w.count == 2 {
		return errors.New("second write failed")
	}
	return w.FileWriter.Apply(change)
}

func TestRollbackOnlyTouchesAppliedEntries(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	if err := os.WriteFile(first, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := &failSecondApplyWriter{}
	engine := Engine{Store: state.New(filepath.Join(root, "state")), Writer: writer}
	changes := []model.Change{
		{AdapterID: "test", Path: first, Scope: model.ScopeUser, Before: []byte("one"), After: []byte("ONE"), Mode: 0o600, Existed: true},
		{AdapterID: "test", Path: second, Scope: model.ScopeUser, Before: []byte("two"), After: []byte("TWO"), Mode: 0o600, Existed: true},
	}
	result := engine.Run(context.Background(), changes, nil)
	if result.Err == nil || result.Manifest.AppliedCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if got, _ := os.ReadFile(first); string(got) != "one" {
		t.Fatalf("first was not restored: %q", got)
	}
	if got, _ := os.ReadFile(second); string(got) != "two" {
		t.Fatalf("unapplied entry was modified: %q", got)
	}
}
