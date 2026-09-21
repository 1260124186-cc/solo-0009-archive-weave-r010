package storage

import (
	"context"
	"sort"
	"sync"

	"example.com/solo-0009-archive-weave/internal/domain"
)

// ComparisonRepository persists comparison jobs. ModifyJob performs a
// read-modify-write under the store lock; mutate returns changed=false to
// abandon a stale optimistic update, which surfaces as a conflict.
type ComparisonRepository interface {
	CreateJob(ctx context.Context, job domain.ComparisonJob) error
	GetJob(ctx context.Context, id string) (domain.ComparisonJob, error)
	ListJobs(ctx context.Context) ([]domain.ComparisonJob, error)
	FindByFingerprint(ctx context.Context, fingerprint string) (domain.ComparisonJob, error)
	ModifyJob(ctx context.Context, id string, mutate func(domain.ComparisonJob) (domain.ComparisonJob, bool, error)) (domain.ComparisonJob, error)
}

type JSONComparisonStore struct {
	path   string
	gate   gate
	jobs   map[string]domain.ComparisonJob
	loaded bool
	loadMu sync.Mutex
}

func NewJSONComparisonStore(path string) *JSONComparisonStore {
	return &JSONComparisonStore{path: path, jobs: map[string]domain.ComparisonJob{}}
}

func (s *JSONComparisonStore) ensureLoaded() error {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	if s.loaded {
		return nil
	}
	values, err := readComparisonJobs(s.path)
	if err != nil {
		return err
	}
	loaded := make(map[string]domain.ComparisonJob, len(values))
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return err
		}
		loaded[value.ID] = value.Clone()
	}
	s.jobs = loaded
	s.loaded = true
	return nil
}

func (s *JSONComparisonStore) CreateJob(ctx context.Context, job domain.ComparisonJob) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := job.Validate(); err != nil {
		return err
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		if _, ok := s.jobs[job.ID]; ok {
			return domain.Conflict("comparison job already exists")
		}
		next := make(map[string]domain.ComparisonJob, len(s.jobs)+1)
		for id, value := range s.jobs {
			next[id] = value.Clone()
		}
		next[job.ID] = job.Clone()
		if err := writeComparisonJobs(s.path, comparisonMapValues(next)); err != nil {
			return err
		}
		s.jobs = next
		return nil
	})
}

func (s *JSONComparisonStore) GetJob(ctx context.Context, id string) (domain.ComparisonJob, error) {
	if err := contextError(ctx); err != nil {
		return domain.ComparisonJob{}, err
	}
	if err := s.ensureLoaded(); err != nil {
		return domain.ComparisonJob{}, err
	}
	found := false
	result := lockedRead(&s.gate, func() domain.ComparisonJob {
		value, ok := s.jobs[id]
		found = ok
		if !ok {
			return domain.ComparisonJob{}
		}
		return value.Clone()
	})
	if !found {
		return domain.ComparisonJob{}, domain.NotFound("comparison job not found")
	}
	return result, nil
}

func (s *JSONComparisonStore) ListJobs(ctx context.Context) ([]domain.ComparisonJob, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	return lockedRead(&s.gate, func() []domain.ComparisonJob {
		result := make([]domain.ComparisonJob, 0, len(s.jobs))
		for _, value := range s.jobs {
			result = append(result, value.Clone())
		}
		sortComparisonJobs(result)
		return result
	}), nil
}

func (s *JSONComparisonStore) FindByFingerprint(ctx context.Context, fingerprint string) (domain.ComparisonJob, error) {
	if err := contextError(ctx); err != nil {
		return domain.ComparisonJob{}, err
	}
	if err := s.ensureLoaded(); err != nil {
		return domain.ComparisonJob{}, err
	}
	found := false
	result := lockedRead(&s.gate, func() domain.ComparisonJob {
		for _, value := range s.jobs {
			if value.Fingerprint == fingerprint {
				found = true
				return value.Clone()
			}
		}
		return domain.ComparisonJob{}
	})
	if !found {
		return domain.ComparisonJob{}, domain.NotFound("comparison job not found")
	}
	return result, nil
}

func (s *JSONComparisonStore) ModifyJob(ctx context.Context, id string, mutate func(domain.ComparisonJob) (domain.ComparisonJob, bool, error)) (domain.ComparisonJob, error) {
	if err := contextError(ctx); err != nil {
		return domain.ComparisonJob{}, err
	}
	if err := s.ensureLoaded(); err != nil {
		return domain.ComparisonJob{}, err
	}
	return lockedWrite2(&s.gate, func() (domain.ComparisonJob, error) {
		current, ok := s.jobs[id]
		if !ok {
			return domain.ComparisonJob{}, domain.NotFound("comparison job not found")
		}
		updated, changed, err := mutate(current.Clone())
		if err != nil {
			return domain.ComparisonJob{}, err
		}
		if !changed {
			return current.Clone(), domain.Conflict("comparison job was modified concurrently")
		}
		if err := updated.Validate(); err != nil {
			return domain.ComparisonJob{}, err
		}
		next := make(map[string]domain.ComparisonJob, len(s.jobs))
		for existingID, value := range s.jobs {
			next[existingID] = value.Clone()
		}
		next[id] = updated.Clone()
		if err := writeComparisonJobs(s.path, comparisonMapValues(next)); err != nil {
			return domain.ComparisonJob{}, err
		}
		s.jobs = next
		return updated.Clone(), nil
	})
}

func comparisonMapValues(values map[string]domain.ComparisonJob) []domain.ComparisonJob {
	result := make([]domain.ComparisonJob, 0, len(values))
	for _, value := range values {
		result = append(result, value.Clone())
	}
	sortComparisonJobs(result)
	return result
}

func sortComparisonJobs(values []domain.ComparisonJob) {
	sort.Slice(values, func(i, j int) bool {
		return values[i].CreatedAt.Before(values[j].CreatedAt) ||
			(values[i].CreatedAt.Equal(values[j].CreatedAt) && values[i].ID < values[j].ID)
	})
}
