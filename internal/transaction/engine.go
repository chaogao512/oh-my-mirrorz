package transaction

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
	"github.com/chaogao512/oh-my-mirrorz/internal/state"
)

type Writer interface {
	Apply(model.Change) error
	Restore(state.Entry, []byte) error
}

type FileWriter struct{}

func (FileWriter) Apply(change model.Change) error {
	current, existed, mode, err := readCurrent(change.Path)
	if err != nil {
		return err
	}
	if existed != change.Existed || state.Sum(current) != state.Sum(change.Before) {
		return fmt.Errorf("configuration changed after plan: %s", change.Path)
	}
	if !change.Changed() {
		return nil
	}
	if change.Remove {
		return os.Remove(change.Path)
	}
	if mode == 0 {
		mode = change.Mode
	}
	return state.AtomicWrite(change.Path, change.After, mode)
}

func (FileWriter) Restore(entry state.Entry, before []byte) error {
	if !entry.Existed {
		err := os.Remove(entry.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return state.AtomicWrite(entry.Path, before, entry.Mode)
}

func readCurrent(path string) ([]byte, bool, fs.FileMode, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, 0, nil
	}
	if err != nil {
		return nil, false, 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, false, 0, fmt.Errorf("refusing symbolic link %s", path)
	}
	data, err := os.ReadFile(path)
	return data, true, info.Mode().Perm(), err
}

type VerifyFunc func(context.Context) error

type Result struct {
	Manifest *state.Manifest
	NoOp     bool
	Err      error
}

type Engine struct {
	Store  *state.Store
	Writer Writer
}

func (e *Engine) Run(ctx context.Context, changes []model.Change, verify VerifyFunc) Result {
	if e.Store == nil {
		return Result{Err: errors.New("transaction store is nil")}
	}
	writer := e.Writer
	if writer == nil {
		writer = FileWriter{}
	}
	if err := validateChanges(changes); err != nil {
		return Result{Err: err}
	}
	lock, err := e.Store.AcquireLock()
	if err != nil {
		return Result{Err: err}
	}
	defer lock.Release()
	return e.runLocked(ctx, changes, verify, "switch", "", writer)
}

func (e *Engine) runLocked(ctx context.Context, changes []model.Change, verify VerifyFunc, kind, targetID string, writer Writer) Result {
	manifest, err := e.Store.CreateWithMetadata(changes, kind, targetID)
	if err != nil {
		return Result{Err: err}
	}
	if err := e.Store.Update(manifest, state.StatusSnapshotted, ""); err != nil {
		return Result{Manifest: manifest, Err: err}
	}
	if err := e.Store.Update(manifest, state.StatusApplying, ""); err != nil {
		return Result{Manifest: manifest, Err: err}
	}
	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return e.fail(manifest, writer, err)
		}
		if err := writer.Apply(change); err != nil {
			return e.fail(manifest, writer, err)
		}
		manifest.AppliedCount++
		if err := e.Store.Update(manifest, state.StatusApplying, ""); err != nil {
			return e.fail(manifest, writer, err)
		}
	}
	if err := e.Store.Update(manifest, state.StatusVerifying, ""); err != nil {
		return e.fail(manifest, writer, err)
	}
	if verify != nil {
		if err := verify(ctx); err != nil {
			return e.fail(manifest, writer, err)
		}
	}
	if err := e.Store.Update(manifest, state.StatusCommitted, ""); err != nil {
		return Result{Manifest: manifest, Err: err}
	}
	return Result{Manifest: manifest}
}

func (e *Engine) Restore(id string) Result {
	if e.Store == nil {
		return Result{Err: errors.New("transaction store is nil")}
	}
	writer := e.Writer
	if writer == nil {
		writer = FileWriter{}
	}
	lock, err := e.Store.AcquireLock()
	if err != nil {
		return Result{Err: err}
	}
	defer lock.Release()
	manifest, err := e.Store.Load(id)
	if err != nil {
		return Result{Err: err}
	}
	restoreChanges, err := e.restoreChanges(manifest)
	if err != nil {
		return Result{Manifest: manifest, Err: err}
	}
	changed := false
	for _, change := range restoreChanges {
		if change.Changed() {
			changed = true
			break
		}
	}
	if !changed {
		return Result{Manifest: manifest, NoOp: true}
	}
	result := e.runLocked(context.Background(), restoreChanges, func(context.Context) error {
		return e.verifyBefore(manifest)
	}, "restore", id, writer)
	if result.Err == nil {
		_ = e.Store.Update(manifest, state.StatusRolledBack, "restored by "+result.Manifest.ID)
	}
	return result
}

func (e *Engine) restoreChanges(manifest *state.Manifest) ([]model.Change, error) {
	changes := make([]model.Change, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		current, existed, mode, err := readCurrent(entry.Path)
		if err != nil {
			return nil, err
		}
		before, err := e.Store.SnapshotBytes(manifest.ID, entry)
		if err != nil {
			return nil, err
		}
		if mode == 0 {
			mode = entry.Mode
		}
		changes = append(changes, model.Change{
			AdapterID: entry.AdapterID,
			Path:      entry.Path,
			Scope:     entry.Scope,
			Before:    current,
			After:     before,
			Mode:      mode,
			Existed:   existed,
			Remove:    !entry.Existed,
		})
	}
	return changes, nil
}

func (e *Engine) verifyBefore(manifest *state.Manifest) error {
	for _, entry := range manifest.Entries {
		current, existed, mode, err := readCurrent(entry.Path)
		if err != nil || existed != entry.Existed || state.Sum(current) != entry.BeforeSHA256 || (entry.Existed && mode.Perm() != entry.Mode.Perm()) {
			return fmt.Errorf("restore verification failed for %s", entry.Path)
		}
	}
	return nil
}

func (e *Engine) fail(manifest *state.Manifest, writer Writer, cause error) Result {
	_ = e.Store.Update(manifest, state.StatusFailed, cause.Error())
	if err := e.Store.Update(manifest, state.StatusRollingBack, cause.Error()); err != nil {
		return Result{Manifest: manifest, Err: fmt.Errorf("%w; persist rollback state: %v", cause, err)}
	}
	if err := e.rollback(manifest, writer); err != nil {
		message := fmt.Sprintf("%v; rollback: %v", cause, err)
		_ = e.Store.Update(manifest, state.StatusDegraded, message)
		return Result{Manifest: manifest, Err: errors.New(message)}
	}
	_ = e.Store.Update(manifest, state.StatusRolledBack, cause.Error())
	return Result{Manifest: manifest, Err: fmt.Errorf("%w; changes rolled back", cause)}
}

func (e *Engine) rollback(manifest *state.Manifest, writer Writer) error {
	var result error
	limit := manifest.AppliedCount
	if limit > len(manifest.Entries) {
		limit = len(manifest.Entries)
	}
	for i := limit - 1; i >= 0; i-- {
		entry := manifest.Entries[i]
		before, err := e.Store.SnapshotBytes(manifest.ID, entry)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if err := writer.Restore(entry, before); err != nil {
			result = errors.Join(result, fmt.Errorf("restore %s: %w", entry.Path, err))
			continue
		}
		current, existed, mode, err := readCurrent(entry.Path)
		if err != nil || existed != entry.Existed || state.Sum(current) != entry.BeforeSHA256 || (entry.Existed && mode.Perm() != entry.Mode.Perm()) {
			result = errors.Join(result, fmt.Errorf("restore verification failed for %s", entry.Path))
		}
	}
	return result
}

func validateChanges(changes []model.Change) error {
	seen := map[string]bool{}
	for _, change := range changes {
		if change.Path == "" || !filepath.IsAbs(change.Path) {
			return fmt.Errorf("invalid change")
		}
		if !fs.ValidPath("x/" + change.AdapterID) {
			return fmt.Errorf("invalid adapter ID %q", change.AdapterID)
		}
		if seen[change.Path] {
			return fmt.Errorf("duplicate change path %s", change.Path)
		}
		seen[change.Path] = true
	}
	return nil
}
