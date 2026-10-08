//go:build unix

package smip

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// FileInbox holds an exclusive OS lock for its lifetime. Its directory must
// be private, on a local filesystem that supports flock/atomic rename/fsync.
type FileInbox struct {
	mu     sync.Mutex
	root   string
	lock   *os.File
	closed bool
}

func OpenInbox(root string) (*FileInbox, error) {
	var err error
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	// lidza:ignore L009 standalone protocol journal on a private persistent volume, not Thura uploads; see docs/protocols/smip-v0.1.md
	if err := os.Mkdir(root, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("inbox directory must be private and not a symlink")
	}
	parent, err := os.Open(filepath.Dir(root))
	if err != nil {
		return nil, err
	}
	err = parent.Sync()
	parent.Close()
	if err != nil {
		return nil, err
	}
	// lidza:ignore L009 OS lock for the standalone single-writer protocol journal, not app object storage
	lock, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("inbox already open or lock unavailable")
	}
	return &FileInbox{root: root, lock: lock}, nil
}
func (s *FileInbox) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.lock.Close()
}
func recordName(origin, id string) string {
	sum := sha256.Sum256([]byte(origin + "\n" + id))
	return hex.EncodeToString(sum[:]) + ".json"
}
func (s *FileInbox) syncDir() error {
	dir, err := os.Open(s.root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// read/write are shared journal primitives. Callers hold mu throughout.
func (s *FileInbox) read(name string, max int64) ([]byte, bool, error) {
	if s.closed {
		return nil, false, errors.New("journal closed")
	}
	f, err := os.Open(filepath.Join(s.root, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > max || info.Mode().Perm()&0077 != 0 {
		return nil, false, errors.New("invalid private journal record")
	}
	b := make([]byte, info.Size())
	if _, err = f.ReadAt(b, 0); err != nil {
		return nil, false, err
	}
	// Resolve uncertain synchronization before returning a durable record.
	if err = f.Sync(); err != nil {
		return nil, false, err
	}
	if err = s.syncDir(); err != nil {
		return nil, false, err
	}
	return b, true, nil
}
func (s *FileInbox) get(origin, id string) (Record, bool, error) {
	b, found, err := s.read(recordName(origin, id), MaxWire+4096)
	if err != nil || !found {
		return Record{}, found, err
	}
	var r Record
	if err = strictJSON(b, &r); err != nil {
		return Record{}, false, err
	}
	if r.Packet.Envelope.From != origin || r.Packet.Envelope.ID != id || r.Packet.Envelope.Validate() != nil || r.Receipt.Digest != r.Packet.Digest() || r.Receipt.ID != id || r.Receipt.From != r.Packet.Envelope.To || r.Receipt.To != origin {
		return Record{}, false, errors.New("corrupt inbox record")
	}
	return r, true, nil
}
func (s *FileInbox) Get(origin, id string) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(origin, id)
}
func (s *FileInbox) Put(r Record) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := r.Packet.Envelope
	old, exists, err := s.get(e.From, e.ID)
	if err != nil {
		return Record{}, false, err
	}
	if exists {
		if old.Packet.Digest() != r.Packet.Digest() {
			return Record{}, false, ErrConflict
		}
		return old, false, nil
	}
	if err = e.Validate(); err != nil {
		return Record{}, false, err
	}
	if r.Receipt.Digest != r.Packet.Digest() || r.Receipt.ID != e.ID || r.Receipt.From != e.To || r.Receipt.To != e.From {
		return Record{}, false, errors.New("receipt does not bind record")
	}
	b, err := json.Marshal(r)
	if err != nil {
		return Record{}, false, err
	}
	if err = s.write(recordName(e.From, e.ID), b); err != nil {
		return Record{}, false, err
	}
	return r, true, nil
}
func (s *FileInbox) write(name string, b []byte) error {
	if s.closed {
		return errors.New("journal closed")
	}
	// lidza:ignore L009 atomic fsync/rename protocol journal; production Thura adapters must use database/storage packs
	f, err := os.CreateTemp(s.root, ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(s.root, name)); err != nil {
		return err
	}
	if err = s.syncDir(); err != nil {
		return err
	}
	return nil
}
