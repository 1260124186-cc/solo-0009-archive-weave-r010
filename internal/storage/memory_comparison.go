package storage

import (
	"context"

	"example.com/solo-0009-archive-weave/internal/domain"
)

// MemorySnapshotStore keeps immutable version snapshots in memory.
type MemorySnapshotStore struct {
	gate      gate
	snapshots map[string]domain.VersionSnapshot
}

func NewMemorySnapshotStore() *MemorySnapshotStore {
	return &MemorySnapshotStore{snapshots: map[string]domain.VersionSnapshot{}}
}

func (s *MemorySnapshotStore) GetVersion(ctx context.Context, artifactID string, version int) (domain.VersionSnapshot, error) {
	if err := contextError(ctx); err != nil {
		return domain.VersionSnapshot{}, err
	}
	found := false
	result := lockedRead(&s.gate, func() domain.VersionSnapshot {
		value, ok := s.snapshots[snapshotKey(artifactID, version)]
		found = ok
		return value
	})
	if !found {
		return domain.VersionSnapshot{}, domain.NotFound("artifact version snapshot not found")
	}
	return result, nil
}

func (s *MemorySnapshotStore) SaveVersion(ctx context.Context, snapshot domain.VersionSnapshot) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		key := snapshotKey(snapshot.ArtifactID, snapshot.Version)
		if _, ok := s.snapshots[key]; ok {
			return nil
		}
		s.snapshots[key] = snapshot
		return nil
	})
}

// MemoryComparisonStore keeps comparison jobs in memory.
type MemoryComparisonStore struct {
	gate gate
	jobs map[string]domain.ComparisonJob
}

func NewMemoryComparisonStore() *MemoryComparisonStore {
	return &MemoryComparisonStore{jobs: map[string]domain.ComparisonJob{}}
}

func (s *MemoryComparisonStore) CreateJob(ctx context.Context, job domain.ComparisonJob) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := job.Validate(); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		if _, ok := s.jobs[job.ID]; ok {
			return domain.Conflict("comparison job already exists")
		}
		s.jobs[job.ID] = job.Clone()
		return nil
	})
}

func (s *MemoryComparisonStore) GetJob(ctx context.Context, id string) (domain.ComparisonJob, error) {
	if err := contextError(ctx); err != nil {
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

func (s *MemoryComparisonStore) ListJobs(ctx context.Context) ([]domain.ComparisonJob, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	result := lockedRead(&s.gate, func() []domain.ComparisonJob {
		jobs := make([]domain.ComparisonJob, 0, len(s.jobs))
		for _, value := range s.jobs {
			jobs = append(jobs, value.Clone())
		}
		sortComparisonJobs(jobs)
		return jobs
	})
	return result, nil
}

func (s *MemoryComparisonStore) FindByFingerprint(ctx context.Context, fingerprint string) (domain.ComparisonJob, error) {
	if err := contextError(ctx); err != nil {
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

func (s *MemoryComparisonStore) ModifyJob(ctx context.Context, id string, mutate func(domain.ComparisonJob) (domain.ComparisonJob, bool, error)) (domain.ComparisonJob, error) {
	if err := contextError(ctx); err != nil {
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
		s.jobs[id] = updated.Clone()
		return updated.Clone(), nil
	})
}
