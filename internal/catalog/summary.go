package catalog

import (
	"context"

	"example.com/solo-0009-archive-weave/internal/domain"
)

func (s *Service) Summary(ctx context.Context) (domain.CatalogSummary, error) {
	values, err := s.repo.List(ctx)
	if err != nil {
		return domain.CatalogSummary{}, err
	}
	return domain.SummarizeArtifacts(values), nil
}
