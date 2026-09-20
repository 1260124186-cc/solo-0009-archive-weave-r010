package storage

import (
	"context"
	"sort"
	"sync"

	"example.com/solo-0009-archive-weave/internal/domain"
)

type JSONStore struct {
	path   string
	gate   gate
	values map[string]domain.Artifact
	loaded bool
	loadMu sync.Mutex
}

func NewJSONStore(path string) *JSONStore {
	return &JSONStore{path: path, values: map[string]domain.Artifact{}}
}

func (s *JSONStore) ensureLoaded() error {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	if s.loaded {
		return nil
	}
	values, err := readArtifacts(s.path)
	if err != nil {
		return err
	}
	loaded := make(map[string]domain.Artifact, len(values))
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return err
		}
		loaded[value.ID] = cloneArtifact(value)
	}
	s.values = loaded
	s.loaded = true
	return nil
}

func (s *JSONStore) List(ctx context.Context) ([]domain.Artifact, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	return lockedRead(&s.gate, func() []domain.Artifact {
		result := make([]domain.Artifact, 0, len(s.values))
		for _, value := range s.values {
			if err := contextError(ctx); err != nil {
				return result
			}
			result = append(result, cloneArtifact(value))
		}
		sortArtifacts(result)
		return result
	}), nil
}

func (s *JSONStore) Get(ctx context.Context, id string) (domain.Artifact, error) {
	if err := contextError(ctx); err != nil {
		return domain.Artifact{}, err
	}
	if err := s.ensureLoaded(); err != nil {
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

func (s *JSONStore) Save(ctx context.Context, artifact domain.Artifact) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := artifact.Validate(); err != nil {
		return err
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		next := make(map[string]domain.Artifact, len(s.values)+1)
		for id, value := range s.values {
			next[id] = cloneArtifact(value)
		}
		next[artifact.ID] = cloneArtifact(artifact)
		values := mapValues(next)
		if err := writeArtifacts(s.path, values); err != nil {
			return err
		}
		s.values = next
		return nil
	})
}

func (s *JSONStore) Delete(ctx context.Context, id string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		if _, ok := s.values[id]; !ok {
			return domain.NotFound("artifact not found")
		}
		next := make(map[string]domain.Artifact, len(s.values))
		for existingID, value := range s.values {
			if existingID != id {
				next[existingID] = cloneArtifact(value)
			}
		}
		if err := writeArtifacts(s.path, mapValues(next)); err != nil {
			return err
		}
		s.values = next
		return nil
	})
}

func mapValues(values map[string]domain.Artifact) []domain.Artifact {
	result := make([]domain.Artifact, 0, len(values))
	for _, value := range values {
		result = append(result, cloneArtifact(value))
	}
	sortArtifacts(result)
	return result
}

func sortArtifacts(values []domain.Artifact) {
	sort.Slice(values, func(i, j int) bool {
		return values[i].ID < values[j].ID
	})
}
