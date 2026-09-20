package storage

import (
	"context"
	"sort"
	"sync"

	"example.com/solo-0009-archive-weave/internal/domain"
)

type JSONAuditStore struct {
	path   string
	gate   gate
	events []domain.AuditEvent
	loaded bool
	loadMu sync.Mutex
}

func NewJSONAuditStore(path string) *JSONAuditStore {
	return &JSONAuditStore{path: path, events: []domain.AuditEvent{}}
}

func (s *JSONAuditStore) ensureLoaded() error {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	if s.loaded {
		return nil
	}
	values, err := readAuditEvents(s.path)
	if err != nil {
		return err
	}
	s.events = make([]domain.AuditEvent, 0, len(values))
	for _, value := range values {
		s.events = append(s.events, cloneAuditEvent(value))
	}
	s.loaded = true
	return nil
}

func (s *JSONAuditStore) Append(ctx context.Context, event domain.AuditEvent) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		for _, existing := range s.events {
			if existing.ID == event.ID {
				return domain.Conflict("audit event already exists")
			}
		}
		next := append(append([]domain.AuditEvent(nil), s.events...), cloneAuditEvent(event))
		sortAuditEvents(next)
		if err := writeAuditEvents(s.path, next); err != nil {
			return err
		}
		s.events = next
		return nil
	})
}

func (s *JSONAuditStore) ListByArtifact(ctx context.Context, artifactID string) ([]domain.AuditEvent, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	return lockedRead(&s.gate, func() []domain.AuditEvent {
		result := make([]domain.AuditEvent, 0)
		for _, event := range s.events {
			if event.ArtifactID == artifactID {
				result = append(result, cloneAuditEvent(event))
			}
		}
		sortAuditEvents(result)
		return result
	}), nil
}

func (s *JSONAuditStore) ListAll(ctx context.Context) ([]domain.AuditEvent, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	return lockedRead(&s.gate, func() []domain.AuditEvent {
		result := make([]domain.AuditEvent, 0, len(s.events))
		for _, event := range s.events {
			result = append(result, cloneAuditEvent(event))
		}
		sortAuditEvents(result)
		return result
	}), nil
}

func (s *JSONAuditStore) DeleteByArtifact(ctx context.Context, artifactID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		next := make([]domain.AuditEvent, 0, len(s.events))
		for _, event := range s.events {
			if event.ArtifactID != artifactID {
				next = append(next, cloneAuditEvent(event))
			}
		}
		if len(next) == len(s.events) {
			return nil
		}
		if err := writeAuditEvents(s.path, next); err != nil {
			return err
		}
		s.events = next
		return nil
	})
}

type MemoryAuditStore struct {
	gate   gate
	events []domain.AuditEvent
}

func NewMemoryAuditStore() *MemoryAuditStore {
	return &MemoryAuditStore{events: []domain.AuditEvent{}}
}

func (s *MemoryAuditStore) Append(ctx context.Context, event domain.AuditEvent) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		for _, existing := range s.events {
			if existing.ID == event.ID {
				return domain.Conflict("audit event already exists")
			}
		}
		s.events = append(s.events, cloneAuditEvent(event))
		sortAuditEvents(s.events)
		return nil
	})
}

func (s *MemoryAuditStore) ListByArtifact(ctx context.Context, artifactID string) ([]domain.AuditEvent, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return lockedRead(&s.gate, func() []domain.AuditEvent {
		result := make([]domain.AuditEvent, 0)
		for _, event := range s.events {
			if event.ArtifactID == artifactID {
				result = append(result, cloneAuditEvent(event))
			}
		}
		sortAuditEvents(result)
		return result
	}), nil
}

func (s *MemoryAuditStore) ListAll(ctx context.Context) ([]domain.AuditEvent, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return lockedRead(&s.gate, func() []domain.AuditEvent {
		result := append([]domain.AuditEvent(nil), s.events...)
		sortAuditEvents(result)
		return result
	}), nil
}

func (s *MemoryAuditStore) DeleteByArtifact(ctx context.Context, artifactID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		next := make([]domain.AuditEvent, 0, len(s.events))
		for _, event := range s.events {
			if event.ArtifactID != artifactID {
				next = append(next, cloneAuditEvent(event))
			}
		}
		s.events = next
		return nil
	})
}

func sortAuditEvents(values []domain.AuditEvent) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].ArtifactID != values[j].ArtifactID {
			return values[i].ArtifactID < values[j].ArtifactID
		}
		if values[i].Version != values[j].Version {
			return values[i].Version < values[j].Version
		}
		return values[i].ID < values[j].ID
	})
}
