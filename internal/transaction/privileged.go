package transaction

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
	"github.com/chaogao512/oh-my-mirrorz/internal/state"
)

// CommandRunner exists so privileged writes can be tested without invoking
// sudo. Arguments are always passed directly to exec, never through a shell.
type CommandRunner interface {
	Run(name string, args ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// PrivilegedWriter delegates user files to FileWriter and uses sudo only for
// system-scoped changes. System replacements are staged and renamed so readers
// never observe a partially written configuration.
type PrivilegedWriter struct {
	Runner     CommandRunner
	SystemRoot string
}

func (w PrivilegedWriter) runner() CommandRunner {
	if w.Runner == nil {
		return ExecRunner{}
	}
	return w.Runner
}

func (w PrivilegedWriter) Apply(change model.Change) error {
	if change.Scope != model.ScopeSystem {
		return (FileWriter{}).Apply(change)
	}
	if !w.allowedSystemPath(change.AdapterID, change.Path) {
		return fmt.Errorf("refusing privileged path outside APT configuration: %s", change.Path)
	}
	current, existed, _, err := readCurrent(change.Path)
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
		return w.runner().Run("sudo", "rm", "-f", "--", change.Path)
	}
	return w.atomicInstall(change.Path, change.After, change.Mode)
}

func (w PrivilegedWriter) Restore(entry state.Entry, before []byte) error {
	if entry.Scope != model.ScopeSystem {
		return (FileWriter{}).Restore(entry, before)
	}
	if !w.allowedSystemPath(entry.AdapterID, entry.Path) {
		return fmt.Errorf("refusing privileged path outside APT configuration: %s", entry.Path)
	}
	if !entry.Existed {
		return w.runner().Run("sudo", "rm", "-f", "--", entry.Path)
	}
	return w.atomicInstall(entry.Path, before, entry.Mode)
}

func (w PrivilegedWriter) allowedSystemPath(adapterID, target string) bool {
	if adapterID != "apt" {
		return false
	}
	root := w.SystemRoot
	if root == "" {
		root = "/"
	}
	target = filepath.Clean(target)
	base := filepath.Join(root, "etc", "apt")
	if target == filepath.Join(base, "sources.list") {
		return true
	}
	dir := filepath.Join(base, "sources.list.d")
	return filepath.Dir(target) == dir && (strings.HasSuffix(target, ".list") || strings.HasSuffix(target, ".sources"))
}

func (w PrivilegedWriter) atomicInstall(target string, data []byte, mode fs.FileMode) error {
	local, err := os.CreateTemp("", "omm-sudo-*")
	if err != nil {
		return err
	}
	localPath := local.Name()
	defer os.Remove(localPath)
	if err := local.Chmod(0o600); err != nil {
		_ = local.Close()
		return err
	}
	if _, err := local.Write(data); err != nil {
		_ = local.Close()
		return err
	}
	if err := local.Sync(); err != nil {
		_ = local.Close()
		return err
	}
	if err := local.Close(); err != nil {
		return err
	}
	staged := filepath.Join(filepath.Dir(target), ".omm-write-"+filepath.Base(localPath))
	permissions := mode.Perm()
	if permissions == 0 {
		permissions = 0o644
	}
	runner := w.runner()
	if err := runner.Run("sudo", "install", "-m", strconv.FormatUint(uint64(permissions), 8), "--", localPath, staged); err != nil {
		return fmt.Errorf("stage privileged configuration: %w", err)
	}
	if err := runner.Run("sudo", "mv", "-f", "--", staged, target); err != nil {
		_ = runner.Run("sudo", "rm", "-f", "--", staged)
		return fmt.Errorf("commit privileged configuration: %w", err)
	}
	return nil
}
