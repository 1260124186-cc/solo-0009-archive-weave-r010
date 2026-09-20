package storage

import (
	"context"

	"example.com/solo-0009-archive-weave/internal/domain"
)

type Repository interface {
	List(ctx context.Context) ([]domain.Artifact, error)
	Get(ctx context.Context, id string) (domain.Artifact, error)
	Save(ctx context.Context, artifact domain.Artifact) error
	Delete(ctx context.Context, id string) error
}

type AuditRepository interface {
	Append(ctx context.Context, event domain.AuditEvent) error
	ListByArtifact(ctx context.Context, artifactID string) ([]domain.AuditEvent, error)
	ListAll(ctx context.Context) ([]domain.AuditEvent, error)
	DeleteByArtifact(ctx context.Context, artifactID string) error
}
