package comparison

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"example.com/solo-0009-archive-weave/internal/domain"
)

// JSONJobStore 把比较任务以确定性 JSON 原子写入磁盘。
type JSONJobStore struct {
	path   string
	mu     readWriteMutex
	jobs   map[string]domain.ComparisonJob
	loaded bool
	loadMu sync.Mutex
}

func NewJSONJobStore(path string) *JSONJobStore {
	return &JSONJobStore{path: path, jobs: map[string]domain.ComparisonJob{}}
}

func (s *JSONJobStore) ensureLoaded() error {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	if s.loaded {
		return nil
	}
	values, err := readJobs(s.path)
	if err != nil {
		return err
	}
	loaded := make(map[string]domain.ComparisonJob, len(values))
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return err
		}
		loaded[value.ID] = cloneJob(value)
	}
	s.jobs = loaded
	s.loaded = true
	return nil
}

func (s *JSONJobStore) Save(ctx context.Context, job domain.ComparisonJob) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if err := job.Validate(); err != nil {
		return err
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	s.mu.lock()
	defer s.mu.unlock()
	next := make(map[string]domain.ComparisonJob, len(s.jobs)+1)
	for id, existing := range s.jobs {
		next[id] = cloneJob(existing)
	}
	next[job.ID] = cloneJob(job)
	if err := writeJobs(s.path, flattenJobs(next)); err != nil {
		return err
	}
	s.jobs = next
	return nil
}

func (s *JSONJobStore) Get(ctx context.Context, id string) (domain.ComparisonJob, error) {
	if err := ctxErr(ctx); err != nil {
		return domain.ComparisonJob{}, err
	}
	if err := s.ensureLoaded(); err != nil {
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

func (s *JSONJobStore) FindFingerprint(ctx context.Context, fingerprint string) (domain.ComparisonJob, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return domain.ComparisonJob{}, false, err
	}
	if err := s.ensureLoaded(); err != nil {
		return domain.ComparisonJob{}, false, err
	}
	s.mu.rLock()
	defer s.mu.rUnlock()
	// 优先复用最近创建的任务，扫描时记录最新候选。
	var best domain.ComparisonJob
	found := false
	for _, job := range s.jobs {
		if job.Fingerprint != fingerprint {
			continue
		}
		if !found || job.CreatedAt.After(best.CreatedAt) {
			best = cloneJob(job)
			found = true
		}
	}
	return best, found, nil
}

func (s *JSONJobStore) List(ctx context.Context, limit int) ([]domain.ComparisonJob, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	s.mu.rLock()
	defer s.mu.rUnlock()
	result := flattenJobs(s.jobs)
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func flattenJobs(jobs map[string]domain.ComparisonJob) []domain.ComparisonJob {
	result := make([]domain.ComparisonJob, 0, len(jobs))
	for _, job := range jobs {
		result = append(result, cloneJob(job))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func readJobs(path string) ([]domain.ComparisonJob, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []domain.ComparisonJob{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) == 0 {
		return []domain.ComparisonJob{}, nil
	}
	var values []domain.ComparisonJob
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if values == nil {
		values = []domain.ComparisonJob{}
	}
	return values, nil
}

func writeJobs(path string, values []domain.ComparisonJob) error {
	if values == nil {
		values = []domain.ComparisonJob{}
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')
	return writeJobFileAtomic(path, data)
}

func writeJobFileAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if directory == "" {
		directory = "."
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", directory, err)
	}
	output, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	temporary := output.Name()
	cleanup := func() {
		_ = output.Close()
		_ = os.Remove(temporary)
	}
	if err := output.Chmod(0o644); err != nil {
		cleanup()
		return fmt.Errorf("set mode on %s: %w", temporary, err)
	}
	if _, err := output.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write %s: %w", temporary, err)
	}
	if err := output.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync %s: %w", temporary, err)
	}
	if err := output.Close(); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("close %s: %w", temporary, err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}
