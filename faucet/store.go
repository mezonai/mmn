package faucet

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type FileStore struct {
	path string
	mu   sync.RWMutex
}

type fileData struct {
	Proposals map[string]*Proposal `json:"proposals"`
}

func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fs := &FileStore{path: filepath.Join(dir, "proposals.json")}
	if _, err := os.Stat(fs.path); errors.Is(err, os.ErrNotExist) {
		b, _ := json.Marshal(fileData{Proposals: map[string]*Proposal{}})
		if err := os.WriteFile(fs.path, b, 0o600); err != nil {
			return nil, err
		}
	}
	return fs, nil
}

func (s *FileStore) load() (*fileData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var d fileData
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	if d.Proposals == nil {
		d.Proposals = map[string]*Proposal{}
	}
	return &d, nil
}

func (s *FileStore) save(d *fileData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}

func (s *FileStore) Upsert(p *Proposal) error {
	d, err := s.load()
	if err != nil {
		return err
	}
	d.Proposals[p.ID] = p
	return s.save(d)
}

func (s *FileStore) Get(id string) (*Proposal, error) {
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	p, ok := d.Proposals[id]
	if !ok {
		return nil, fmt.Errorf("proposal %s not found", id)
	}
	return p, nil
}

func (s *FileStore) List() ([]*Proposal, error) {
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	res := make([]*Proposal, 0, len(d.Proposals))
	for _, p := range d.Proposals {
		res = append(res, p)
	}
	return res, nil
}
