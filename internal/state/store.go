package state

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/chaogao512/oh-my-mirrorz/internal/model"
)

type Status string

const (
	StatusPrepared    Status = "prepared"
	StatusSnapshotted Status = "snapshotted"
	StatusApplying    Status = "applying"
	StatusVerifying   Status = "verifying"
	StatusCommitted   Status = "committed"
	StatusFailed      Status = "failed"
	StatusRollingBack Status = "rolling-back"
	StatusRolledBack  Status = "rolled-back"
	StatusDegraded    Status = "degraded"
)

type Entry struct {
	AdapterID    string      `json:"adapter_id"`
	Path         string      `json:"path"`
	Scope        model.Scope `json:"scope"`
	Existed      bool        `json:"existed"`
	Mode         fs.FileMode `json:"mode"`
	BeforeSHA256 string      `json:"before_sha256"`
	AfterSHA256  string      `json:"after_sha256"`
	SnapshotFile string      `json:"snapshot_file,omitempty"`
}

type Manifest struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	TargetID     string    `json:"target_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Status       Status    `json:"status"`
	Error        string    `json:"error,omitempty"`
	AppliedCount int       `json:"applied_count,omitempty"`
	Entries      []Entry   `json:"entries"`
}

type Store struct {
	Root  string
	Clock func() time.Time
}

func New(root string) *Store {
	return &Store{Root: root, Clock: time.Now}
}

func (s *Store) Ensure() error {
	if s.Root == "" {
		return errors.New("state root is empty")
	}
	return os.MkdirAll(filepath.Join(s.Root, "transactions"), 0o700)
}

func (s *Store) Create(changes []model.Change) (*Manifest, error) {
	return s.CreateWithMetadata(changes, "switch", "")
}

func (s *Store) CreateWithMetadata(changes []model.Change, kind, targetID string) (*Manifest, error) {
	if err := s.Ensure(); err != nil {
		return nil, err
	}
	id, err := s.newID()
	if err != nil {
		return nil, err
	}
	dir := s.transactionDir(id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(dir)
		}
	}()
	now := s.now().UTC()
	manifest := &Manifest{ID: id, Kind: kind, TargetID: targetID, CreatedAt: now, UpdatedAt: now, Status: StatusPrepared}
	for i, change := range changes {
		entry := Entry{
			AdapterID:    change.AdapterID,
			Path:         change.Path,
			Scope:        change.Scope,
			Existed:      change.Existed,
			Mode:         change.Mode,
			BeforeSHA256: Sum(change.Before),
			AfterSHA256:  Sum(change.After),
		}
		if change.Existed {
			entry.SnapshotFile = fmt.Sprintf("%03d.snapshot", i)
			if err := os.WriteFile(filepath.Join(dir, entry.SnapshotFile), change.Before, 0o600); err != nil {
				return nil, err
			}
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	if err := s.writeManifest(manifest); err != nil {
		return nil, err
	}
	complete = true
	return manifest, nil
}

func (s *Store) Update(manifest *Manifest, status Status, message string) error {
	manifest.Status = status
	manifest.Error = message
	manifest.UpdatedAt = s.now().UTC()
	return s.writeManifest(manifest)
}

func (s *Store) Load(id string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(s.transactionDir(id), "manifest.json"))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.ID != id {
		return nil, fmt.Errorf("manifest ID mismatch")
	}
	if !validStatus(manifest.Status) {
		return nil, fmt.Errorf("invalid transaction status %q", manifest.Status)
	}
	if manifest.Kind != "switch" && manifest.Kind != "restore" {
		return nil, fmt.Errorf("invalid transaction kind %q", manifest.Kind)
	}
	if manifest.AppliedCount < 0 || manifest.AppliedCount > len(manifest.Entries) {
		return nil, fmt.Errorf("invalid applied entry count")
	}
	for _, entry := range manifest.Entries {
		if entry.Path == "" || !filepath.IsAbs(entry.Path) {
			return nil, fmt.Errorf("invalid transaction entry path")
		}
		if entry.AdapterID == "" || (entry.Scope != model.ScopeUser && entry.Scope != model.ScopeSystem) || !validDigest(entry.BeforeSHA256) || !validDigest(entry.AfterSHA256) {
			return nil, fmt.Errorf("invalid transaction entry metadata")
		}
		if entry.Existed && (entry.SnapshotFile == "" || filepath.Base(entry.SnapshotFile) != entry.SnapshotFile) {
			return nil, fmt.Errorf("invalid transaction snapshot filename")
		}
	}
	return &manifest, nil
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validStatus(status Status) bool {
	switch status {
	case StatusPrepared, StatusSnapshotted, StatusApplying, StatusVerifying, StatusCommitted, StatusFailed, StatusRollingBack, StatusRolledBack, StatusDegraded:
		return true
	default:
		return false
	}
}

func (s *Store) SnapshotBytes(id string, entry Entry) ([]byte, error) {
	if !entry.Existed {
		return nil, nil
	}
	if entry.SnapshotFile == "" || filepath.Base(entry.SnapshotFile) != entry.SnapshotFile {
		return nil, fmt.Errorf("invalid snapshot filename")
	}
	data, err := os.ReadFile(filepath.Join(s.transactionDir(id), entry.SnapshotFile))
	if err != nil {
		return nil, err
	}
	if Sum(data) != entry.BeforeSHA256 {
		return nil, fmt.Errorf("snapshot checksum mismatch for %s", entry.Path)
	}
	return data, nil
}

func (s *Store) List() ([]Manifest, error) {
	if err := s.Ensure(); err != nil {
		return nil, err
	}
	items, err := os.ReadDir(filepath.Join(s.Root, "transactions"))
	if err != nil {
		return nil, err
	}
	result := make([]Manifest, 0, len(items))
	for _, item := range items {
		if !item.IsDir() {
			continue
		}
		manifest, err := s.Load(item.Name())
		if err != nil {
			return nil, fmt.Errorf("load transaction %s: %w", item.Name(), err)
		}
		for _, entry := range manifest.Entries {
			if _, err := s.SnapshotBytes(manifest.ID, entry); err != nil {
				return nil, fmt.Errorf("validate transaction %s: %w", item.Name(), err)
			}
		}
		result = append(result, *manifest)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

type Lock struct {
	path string
	file *os.File
}

func (s *Store) AcquireLock() (*Lock, error) {
	if err := s.Ensure(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.Root, "transaction.lock")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("another transaction is active")
		}
		return nil, err
	}
	_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
	return &Lock{path: path, file: file}, nil
}

func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	closeErr := l.file.Close()
	removeErr := os.Remove(l.path)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

func (s *Store) writeManifest(manifest *Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return AtomicWrite(filepath.Join(s.transactionDir(manifest.ID), "manifest.json"), data, 0o600)
}

func (s *Store) transactionDir(id string) string {
	return filepath.Join(s.Root, "transactions", filepath.Base(id))
}

func (s *Store) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

func (s *Store) newID() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return s.now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random), nil
}

func Sum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func AtomicWrite(path string, data []byte, mode fs.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symbolic link %s", path)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".omm-write-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	ok := false
	defer func() {
		_ = temp.Close()
		if !ok {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(mode.Perm()); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	ok = true
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
	return nil
}
