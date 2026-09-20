package storage

import (
	"context"
	"sort"
	"sync"

	"example.com/solo-0009-archive-weave/internal/domain"
)

// SnapshotRepository 保存档案每个版本的不可变快照。
// 比较任务在创建时绑定具体版本，之后的档案修订不能改写已保存的结论。
type SnapshotRepository interface {
	// PutVersion 幂等保存某档案某版本的快照；同一 (id, version) 内容不同即冲突。
	PutVersion(ctx context.Context, artifact domain.Artifact) error
	// GetVersion 读取某档案某版本；不存在时返回 not_found。
	GetVersion(ctx context.Context, id string, version int) (domain.Artifact, error)
	// LatestVersion 返回某档案当前已知的最大版本；不存在时返回 not_found。
	LatestVersion(ctx context.Context, id string) (domain.Artifact, error)
	// HasVersion 判断某档案某版本是否已有快照。
	HasVersion(ctx context.Context, id string, version int) (bool, error)
}

type snapshotIndex map[string]map[int]domain.Artifact

// JSONSnapshotStore 把版本快照确定性地写入 JSON 文件。
type JSONSnapshotStore struct {
	path   string
	gate   gate
	values snapshotIndex
	loaded bool
	loadMu sync.Mutex
}

func NewJSONSnapshotStore(path string) *JSONSnapshotStore {
	return &JSONSnapshotStore{path: path, values: snapshotIndex{}}
}

func (s *JSONSnapshotStore) ensureLoaded() error {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	if s.loaded {
		return nil
	}
	values, err := readSnapshots(s.path)
	if err != nil {
		return err
	}
	loaded := snapshotIndex{}
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return err
		}
		byID := loaded[value.ID]
		if byID == nil {
			byID = map[int]domain.Artifact{}
			loaded[value.ID] = byID
		}
		byID[value.Version] = cloneArtifact(value)
	}
	s.values = loaded
	s.loaded = true
	return nil
}

func (s *JSONSnapshotStore) PutVersion(ctx context.Context, artifact domain.Artifact) error {
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
		if byID := s.values[artifact.ID]; byID != nil {
			if _, exists := byID[artifact.Version]; exists {
				// 版本号单调递增，同一版本重放视为幂等无操作。
				return nil
			}
		}
		next := cloneSnapshotIndex(s.values)
		byID := next[artifact.ID]
		if byID == nil {
			byID = map[int]domain.Artifact{}
			next[artifact.ID] = byID
		}
		byID[artifact.Version] = cloneArtifact(artifact)
		if err := writeSnapshots(s.path, flattenSnapshotIndex(next)); err != nil {
			return err
		}
		s.values = next
		return nil
	})
}

type snapshotLookup struct {
	artifact domain.Artifact
	found    bool
}

func (s *JSONSnapshotStore) get(id string, version int, latest bool) (domain.Artifact, bool) {
	result := lockedRead(&s.gate, func() snapshotLookup {
		byID, ok := s.values[id]
		if !ok || len(byID) == 0 {
			return snapshotLookup{}
		}
		if latest {
			version = 0
			for v := range byID {
				if v > version {
					version = v
				}
			}
		}
		artifact, ok := byID[version]
		if !ok {
			return snapshotLookup{}
		}
		return snapshotLookup{artifact: cloneArtifact(artifact), found: true}
	})
	return result.artifact, result.found
}

// RemoveVersion 删除单个版本快照，仅供装饰器在档案写入失败时清理孤儿快照。
func (s *JSONSnapshotStore) RemoveVersion(ctx context.Context, id string, version int) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		byID, ok := s.values[id]
		if !ok {
			return nil
		}
		if _, exists := byID[version]; !exists {
			return nil
		}
		next := cloneSnapshotIndex(s.values)
		delete(next[id], version)
		if len(next[id]) == 0 {
			delete(next, id)
		}
		if err := writeSnapshots(s.path, flattenSnapshotIndex(next)); err != nil {
			return err
		}
		s.values = next
		return nil
	})
}

func (s *JSONSnapshotStore) GetVersion(ctx context.Context, id string, version int) (domain.Artifact, error) {
	if err := contextError(ctx); err != nil {
		return domain.Artifact{}, err
	}
	if err := s.ensureLoaded(); err != nil {
		return domain.Artifact{}, err
	}
	artifact, found := s.get(id, version, false)
	if !found {
		return domain.Artifact{}, domain.NotFound("artifact version snapshot not found")
	}
	return artifact, nil
}

func (s *JSONSnapshotStore) LatestVersion(ctx context.Context, id string) (domain.Artifact, error) {
	if err := contextError(ctx); err != nil {
		return domain.Artifact{}, err
	}
	if err := s.ensureLoaded(); err != nil {
		return domain.Artifact{}, err
	}
	artifact, found := s.get(id, 0, true)
	if !found {
		return domain.Artifact{}, domain.NotFound("artifact snapshot not found")
	}
	return artifact, nil
}

func (s *JSONSnapshotStore) HasVersion(ctx context.Context, id string, version int) (bool, error) {
	if err := contextError(ctx); err != nil {
		return false, err
	}
	if err := s.ensureLoaded(); err != nil {
		return false, err
	}
	return lockedRead(&s.gate, func() bool {
		byID, ok := s.values[id]
		if !ok {
			return false
		}
		_, ok = byID[version]
		return ok
	}), nil
}

// MemorySnapshotStore 是进程内版本快照库。
type MemorySnapshotStore struct {
	gate   gate
	values snapshotIndex
}

func NewMemorySnapshotStore() *MemorySnapshotStore {
	return &MemorySnapshotStore{values: snapshotIndex{}}
}

func (s *MemorySnapshotStore) PutVersion(ctx context.Context, artifact domain.Artifact) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := artifact.Validate(); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		byID := s.values[artifact.ID]
		if byID == nil {
			byID = map[int]domain.Artifact{}
			s.values[artifact.ID] = byID
		}
		if _, ok := byID[artifact.Version]; ok {
			return nil
		}
		byID[artifact.Version] = cloneArtifact(artifact)
		return nil
	})
}

// RemoveVersion 删除单个版本快照，仅供装饰器清理孤儿快照。
func (s *MemorySnapshotStore) RemoveVersion(ctx context.Context, id string, version int) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		if byID, ok := s.values[id]; ok {
			delete(byID, version)
			if len(byID) == 0 {
				delete(s.values, id)
			}
		}
		return nil
	})
}

func (s *MemorySnapshotStore) GetVersion(ctx context.Context, id string, version int) (domain.Artifact, error) {
	if err := contextError(ctx); err != nil {
		return domain.Artifact{}, err
	}
	found := false
	result := lockedRead(&s.gate, func() domain.Artifact {
		if byID, ok := s.values[id]; ok {
			if artifact, ok := byID[version]; ok {
				found = true
				return cloneArtifact(artifact)
			}
		}
		return domain.Artifact{}
	})
	if !found {
		return domain.Artifact{}, domain.NotFound("artifact version snapshot not found")
	}
	return result, nil
}

func (s *MemorySnapshotStore) LatestVersion(ctx context.Context, id string) (domain.Artifact, error) {
	if err := contextError(ctx); err != nil {
		return domain.Artifact{}, err
	}
	found := false
	result := lockedRead(&s.gate, func() domain.Artifact {
		byID, ok := s.values[id]
		if !ok || len(byID) == 0 {
			return domain.Artifact{}
		}
		version := 0
		for v := range byID {
			if v > version {
				version = v
			}
		}
		artifact, ok := byID[version]
		if !ok {
			return domain.Artifact{}
		}
		found = true
		return cloneArtifact(artifact)
	})
	if !found {
		return domain.Artifact{}, domain.NotFound("artifact snapshot not found")
	}
	return result, nil
}

func (s *MemorySnapshotStore) HasVersion(ctx context.Context, id string, version int) (bool, error) {
	if err := contextError(ctx); err != nil {
		return false, err
	}
	return lockedRead(&s.gate, func() bool {
		byID, ok := s.values[id]
		if !ok {
			return false
		}
		_, ok = byID[version]
		return ok
	}), nil
}

func cloneSnapshotIndex(values snapshotIndex) snapshotIndex {
	next := make(snapshotIndex, len(values))
	for id, byID := range values {
		cloned := make(map[int]domain.Artifact, len(byID))
		for version, artifact := range byID {
			cloned[version] = cloneArtifact(artifact)
		}
		next[id] = cloned
	}
	return next
}

func flattenSnapshotIndex(values snapshotIndex) []domain.Artifact {
	result := make([]domain.Artifact, 0)
	for _, byID := range values {
		for _, artifact := range byID {
			result = append(result, cloneArtifact(artifact))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID != result[j].ID {
			return result[i].ID < result[j].ID
		}
		return result[i].Version < result[j].Version
	})
	return result
}
