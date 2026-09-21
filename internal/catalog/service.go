package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"example.com/solo-0009-archive-weave/internal/domain"
	"example.com/solo-0009-archive-weave/internal/storage"
)

const DefaultActor = "archive-service"

type Service struct {
	repo      storage.Repository
	audit     storage.AuditRepository
	snapshots storage.VersionSnapshotRepository
	policy    Policy
	clock     func() time.Time
	nextID    func() string
}

func NewService(repo storage.Repository, audit storage.AuditRepository) *Service {
	if audit == nil {
		audit = storage.NewMemoryAuditStore()
	}
	return &Service{
		repo:   repo,
		audit:  audit,
		policy: DefaultPolicy(),
		clock:  time.Now,
		nextID: uuid.NewString,
	}
}

// WithVersionSnapshots enables immutable (artifact id, version) snapshots on
// every successful write. Comparations resolve historical versions from this
// store, which keeps them bound to the version seen at submission time.
func (s *Service) WithVersionSnapshots(repository storage.VersionSnapshotRepository) *Service {
	s.snapshots = repository
	return s
}

// recordSnapshot persists the artifact version. A write failure forces the
// caller's existing artifact/audit rollback so the durable state never loses a
// version that a later comparison could try to resolve.
func (s *Service) recordSnapshot(ctx context.Context, artifact domain.Artifact) error {
	if s.snapshots == nil {
		return nil
	}
	snapshot := domain.VersionSnapshot{
		ArtifactID: artifact.ID,
		Version:    artifact.Version,
		Artifact:   artifact.Clone(),
		CapturedAt: s.clock().UTC(),
	}
	return s.snapshots.SaveVersion(ctx, snapshot)
}

func (s *Service) actor(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultActor
	}
	return value
}

func (s *Service) applyMutation(
	ctx context.Context,
	id string,
	action domain.TransitionAction,
	actor string,
	note string,
	mutate func(domain.Artifact) (domain.Artifact, error),
) (domain.Artifact, error) {
	before, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.Artifact{}, err
	}
	after, err := mutate(before.Clone())
	if err != nil {
		return domain.Artifact{}, err
	}
	if after.ID != before.ID {
		return domain.Artifact{}, domain.State("mutation cannot change artifact identity")
	}
	if err := after.Validate(); err != nil {
		return domain.Artifact{}, err
	}
	if err := s.repo.Save(ctx, after); err != nil {
		return domain.Artifact{}, err
	}
	event, err := domain.NewAuditEvent(s.nextID(), before, after, action, s.actor(actor), note, after.UpdatedAt)
	if err == nil {
		err = s.audit.Append(ctx, event)
	}
	if err == nil {
		err = s.recordSnapshot(ctx, after)
	}
	if err != nil {
		rollbackErr := s.repo.Save(ctx, before)
		if rollbackErr != nil {
			return domain.Artifact{}, errors.Join(err, fmt.Errorf("rollback artifact: %w", rollbackErr))
		}
		return domain.Artifact{}, err
	}
	return after, nil
}
