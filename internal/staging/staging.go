// Package staging stores uploaded file bodies until a tool consumes them.
package staging

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound = errors.New("upload id not found or expired")
	ErrTooLarge = errors.New("file exceeds the upload limit")
	ErrEmpty    = errors.New("refusing to stage an empty file")
	ErrNotText  = errors.New("staged file is not valid UTF-8")
)

// Info describes one staged file.
type Info struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

type Store struct {
	dir     string
	ttl     time.Duration
	maxSize int64

	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	info    Info
	path    string
	expires time.Time
}

// New creates a store. An empty dir creates a private temp directory.
func New(dir string, ttl time.Duration, maxBytes int64) (*Store, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	if dir == "" {
		created, err := os.MkdirTemp("", "obsitrace-uploads-*")
		if err != nil {
			return nil, err
		}
		dir = created
	} else if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir, ttl: ttl, maxSize: maxBytes, entries: map[string]*entry{}}, nil
}

// Stage reads the body, validates it is UTF-8 text, and returns its id.
func (s *Store) Stage(r io.Reader, filename string) (Info, error) {
	data, err := io.ReadAll(io.LimitReader(r, s.maxSize+1))
	if err != nil {
		return Info{}, err
	}
	if len(data) == 0 {
		return Info{}, ErrEmpty
	}
	if int64(len(data)) > s.maxSize {
		return Info{}, ErrTooLarge
	}
	if !utf8.Valid(data) {
		return Info{}, ErrNotText
	}

	id, err := newID()
	if err != nil {
		return Info{}, err
	}
	path := filepath.Join(s.dir, id)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Info{}, fmt.Errorf("staging file: %w", err)
	}

	sum := sha256.Sum256(data)
	info := Info{
		ID:       id,
		Filename: filepath.Base(filename),
		Size:     int64(len(data)),
		SHA256:   hex.EncodeToString(sum[:]),
	}

	s.mu.Lock()
	s.purgeLocked(time.Now())
	s.entries[id] = &entry{info: info, path: path, expires: time.Now().Add(s.ttl)}
	s.mu.Unlock()
	return info, nil
}

// Read returns the staged bytes without consuming the id, so a failed tool call
// can be retried.
func (s *Store) Read(id string) ([]byte, error) {
	s.mu.Lock()
	e, ok := s.entries[id]
	if !ok {
		s.mu.Unlock()
		return nil, ErrNotFound
	}
	if time.Now().After(e.expires) {
		delete(s.entries, id)
		s.mu.Unlock()
		_ = os.Remove(e.path)
		return nil, ErrNotFound
	}
	path := e.path
	s.mu.Unlock()
	return os.ReadFile(path)
}

// Delete consumes an id after a successful write.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return ErrNotFound
	}
	delete(s.entries, id)
	return os.Remove(e.path)
}

func (s *Store) purgeLocked(now time.Time) {
	for id, e := range s.entries {
		if now.After(e.expires) {
			delete(s.entries, id)
			_ = os.Remove(e.path)
		}
	}
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
