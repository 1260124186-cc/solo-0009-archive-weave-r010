package comparison

import (
	"context"
	"sort"

	"example.com/solo-0009-archive-weave/internal/domain"
)

// JobStore 持久化比较任务，使服务重启后仍可查看或恢复未结束的比较。
type JobStore interface {
	Save(ctx context.Context, job domain.ComparisonJob) error
	Get(ctx context.Context, id string) (domain.ComparisonJob, error)
	// FindFingerprint 返回具有相同输入指纹的既有任务（任意状态），用于结果复用。
	FindFingerprint(ctx context.Context, fingerprint string) (domain.ComparisonJob, bool, error)
	List(ctx context.Context, limit int) ([]domain.ComparisonJob, error)
}

// jobGate 提供与 storage 包一致风格的读写保护，但 comparison 包保持自包含。
type memoryJobStore struct {
	mu   readWriteMutex
	jobs map[string]domain.ComparisonJob
}

// NewMemoryJobStore 创建进程内比较任务存储。
func NewMemoryJobStore() JobStore {
	return &memoryJobStore{jobs: map[string]domain.ComparisonJob{}}
}

func (s *memoryJobStore) Save(ctx context.Context, job domain.ComparisonJob) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := job.Validate(); err != nil {
		return err
	}
	s.mu.lock()
	defer s.mu.unlock()
	s.jobs[job.ID] = cloneJob(job)
	return nil
}

func (s *memoryJobStore) Get(ctx context.Context, id string) (domain.ComparisonJob, error) {
	if err := ctxErr(ctx); err != nil {
		return domain.ComparisonJob{}, err
	}
	s.mu.rLock()
	defer s.mu.rUnlock()
	job, ok := s.jobs[id]
	if !ok {
		return domain.ComparisonJob{}, domain.NotFound("comparison job not found")
	}
	return cloneJob(job), nil
}

func (s *memoryJobStore) FindFingerprint(ctx context.Context, fingerprint string) (domain.ComparisonJob, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return domain.ComparisonJob{}, false, err
	}
	s.mu.rLock()
	defer s.mu.rUnlock()
	for _, job := range s.jobs {
		if job.Fingerprint == fingerprint {
			return cloneJob(job), true, nil
		}
	}
	return domain.ComparisonJob{}, false, nil
}

func (s *memoryJobStore) List(ctx context.Context, limit int) ([]domain.ComparisonJob, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	s.mu.rLock()
	defer s.mu.rUnlock()
	result := make([]domain.ComparisonJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		result = append(result, cloneJob(job))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
