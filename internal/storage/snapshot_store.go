package storage

import (
	"context"
	"sort"
	"strconv"
	"sync"

	"example.com/solo-0009-archive-weave/internal/domain"
)

type VersionSnapshotRepository interface {
	GetVersion(ctx context.Context, artifactID string, version int) (domain.VersionSnapshot, error)
	SaveVersion(ctx context.Context, snapshot domain.VersionSnapshot) error
}

// JSONSnapshotStore persists (artifact id, version) snapshots in one JSON array.
// Writes are immutable: saving an existing version is a no-op so replays after a
// crash stay idempotent.
type JSONSnapshotStore struct {
	path      string
	gate      gate
	snapshots map[string]domain.VersionSnapshot
	loaded    bool
	loadMu    sync.Mutex
}

func NewJSONSnapshotStore(path string) *JSONSnapshotStore {
	return &JSONSnapshotStore{path: path, snapshots: map[string]domain.VersionSnapshot{}}
}

func snapshotKey(artifactID string, version int) string {
	return artifactID + "#v" + strconv.Itoa(version)
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
	loaded := make(map[string]domain.VersionSnapshot, len(values))
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return err
		}
		loaded[snapshotKey(value.ArtifactID, value.Version)] = value
	}
	s.snapshots = loaded
	s.loaded = true
	return nil
}

func (s *JSONSnapshotStore) GetVersion(ctx context.Context, artifactID string, version int) (domain.VersionSnapshot, error) {
	if err := contextError(ctx); err != nil {
		return domain.VersionSnapshot{}, err
	}
	if err := s.ensureLoaded(); err != nil {
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

func (s *JSONSnapshotStore) SaveVersion(ctx context.Context, snapshot domain.VersionSnapshot) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	return lockedWrite(&s.gate, func() error {
		key := snapshotKey(snapshot.ArtifactID, snapshot.Version)
		if _, ok := s.snapshots[key]; ok {
			return nil
		}
		next := make(map[string]domain.VersionSnapshot, len(s.snapshots)+1)
		for key, value := range s.snapshots {
			next[key] = value
		}
		next[key] = snapshot
		if err := writeSnapshots(s.path, snapshotMapValues(next)); err != nil {
			return err
		}
		s.snapshots = next
		return nil
	})
}

func snapshotMapValues(values map[string]domain.VersionSnapshot) []domain.VersionSnapshot {
	result := make([]domain.VersionSnapshot, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sortSnapshots(result)
	return result
}

func sortSnapshots(values []domain.VersionSnapshot) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].ArtifactID != values[j].ArtifactID {
			return values[i].ArtifactID < values[j].ArtifactID
		}
		return values[i].Version < values[j].Version
	})
}
