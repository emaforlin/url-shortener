package links

import (
	"context"
	"fmt"
	"sync"
)

// MemoryStore is an in-process [Store] backed by a map.
//
// It is safe for concurrent use but does not survive a restart. It exists so the
// service runs and is testable with no external dependency; replace it with a
// database-backed Store when persistence is needed. Nothing outside this package
// depends on it beyond the wiring in main.
type MemoryStore struct {
	mu    sync.RWMutex
	links map[string]Link
}

// NewMemoryStore returns an empty store ready for use.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{links: make(map[string]Link)}
}

// Create stores l, or returns ErrAlreadyExists when the code is taken.
func (s *MemoryStore) Create(ctx context.Context, l Link) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.links[l.Code]; exists {
		return fmt.Errorf("create %q: %w", l.Code, ErrAlreadyExists)
	}
	s.links[l.Code] = l
	return nil
}

// GetByCode returns the link for code, or ErrNotFound.
func (s *MemoryStore) GetByCode(ctx context.Context, code string) (Link, error) {
	if err := ctx.Err(); err != nil {
		return Link{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	l, ok := s.links[code]
	if !ok {
		return Link{}, fmt.Errorf("get %q: %w", code, ErrNotFound)
	}
	return l, nil
}

// Delete removes the link for code, or returns ErrNotFound.
func (s *MemoryStore) Delete(ctx context.Context, code string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.links[code]; !ok {
		return fmt.Errorf("delete %q: %w", code, ErrNotFound)
	}
	delete(s.links, code)
	return nil
}

// IncrementHits records one redirect for code.
func (s *MemoryStore) IncrementHits(ctx context.Context, code string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.links[code]
	if !ok {
		return fmt.Errorf("increment hits %q: %w", code, ErrNotFound)
	}
	// Link is a value type, so the counter must be written back into the map.
	l.Hits++
	s.links[code] = l
	return nil
}

// Ping always succeeds: an in-process map is reachable whenever the process is.
func (s *MemoryStore) Ping(ctx context.Context) error {
	return ctx.Err()
}

// Len returns the number of stored links. Intended for tests and diagnostics.
func (s *MemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.links)
}

// Compile-time proof that MemoryStore satisfies the Store contract.
var _ Store = (*MemoryStore)(nil)
