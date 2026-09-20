package storage

import (
	"context"

	"example.com/solo-0009-archive-weave/internal/domain"
)

type MemoryStore struct {
	gate   gate
	values map[string]domain.Artifact
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{values: map[string]domain.Artifact{}}
}

func (s *MemoryStore) List(ctx context.Context) ([]domain.Artifact, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return lockedRead(&s.gate, func() []domain.Artifact {
		result := make([]domain.Artifact, 0, len(s.values))
		for _, value := range s.values {
			result = append(result, cloneArtifact(value))
		}
		sortArtifacts(result)
		return result
	}), nil
}

func (s *MemoryStore) Get(ctx context.Context, id string) (domain.Artifact, error) {
	if err := contextError(ctx); err != nil {
		return domain.Artifact{}, err
	}
	found := false
	result := lockedRead(&s.gate, func() domain.Artifact {
		value, ok := s.values[id]
		found = ok
		if !ok {
			return domain.Artifact{}
		}
		return cloneArtifact(value)
	})
	if !found {
		return domain.Artifact{}, domain.NotFound("artifact not found")
	}
	return result, nil
}

func (s *MemoryStore) Save(ctx context.Context, value domain.Artifact) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		s.values[value.ID] = cloneArtifact(value)
		return nil
	})
}

func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		if _, ok := s.values[id]; !ok {
			return domain.NotFound("artifact not found")
		}
		delete(s.values, id)
		return nil
	})
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
