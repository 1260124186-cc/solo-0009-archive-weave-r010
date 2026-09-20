package catalog

import (
	"context"

	"example.com/solo-0009-archive-weave/internal/domain"
)

func (s *Service) History(ctx context.Context, artifactID string) (domain.Timeline, error) {
	if _, err := s.Get(ctx, artifactID); err != nil {
		return domain.Timeline{}, err
	}
	events, err := s.audit.ListByArtifact(ctx, artifactID)
	if err != nil {
		return domain.Timeline{}, err
	}
	return domain.NewTimeline(artifactID, events), nil
}

func (s *Service) AllHistory(ctx context.Context) ([]domain.AuditEvent, error) {
	return s.audit.ListAll(ctx)
}
