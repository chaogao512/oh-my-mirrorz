package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

func TestCreateAndLoadSnapshot(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	path := filepath.Join(root, "config")
	change := model.Change{AdapterID: "npm", Path: path, Scope: model.ScopeUser, Existed: true, Mode: 0o640, Before: []byte("before"), After: []byte("after")}
	manifest, err := store.Create([]model.Change{change})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.SnapshotBytes(loaded.ID, loaded.Entries[0])
	if err != nil || string(data) != "before" {
		t.Fatalf("snapshot = %q, %v", data, err)
	}
}

func TestAtomicWriteRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(link, []byte("unsafe"), 0o600); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestLockIsExclusive(t *testing.T) {
	store := New(t.TempDir())
	lock, err := store.AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if _, err := store.AcquireLock(); err == nil {
		t.Fatal("expected lock conflict")
	}
}

func TestListBlocksOnCorruptTransaction(t *testing.T) {
	store := New(t.TempDir())
	bad := filepath.Join(store.Root, "transactions", "incomplete")
	if err := os.MkdirAll(bad, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "manifest.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Fatal("corrupt transaction must block history and new writes")
	}
}

func TestListBlocksOnMissingSnapshot(t *testing.T) {
	store := New(t.TempDir())
	change := model.Change{AdapterID: "npm", Path: filepath.Join(store.Root, "config"), Scope: model.ScopeUser, Existed: true, Mode: 0o600, Before: []byte("before"), After: []byte("after")}
	manifest, err := store.Create([]model.Change{change})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(store.transactionDir(manifest.ID), manifest.Entries[0].SnapshotFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Fatal("missing snapshot must block history and new writes")
	}
}
