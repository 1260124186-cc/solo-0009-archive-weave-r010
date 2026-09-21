package catalog

import (
	"context"
	"errors"
	"fmt"

	"example.com/solo-0009-archive-weave/internal/domain"
)

type BatchResult struct {
	Count     int               `json:"count"`
	Artifacts []domain.Artifact `json:"artifacts"`
}

func (s *Service) ImportBatch(ctx context.Context, inputs []domain.CreateArtifact, actor string) (BatchResult, error) {
	if err := s.policy.CheckBatchSize(len(inputs)); err != nil {
		return BatchResult{}, err
	}
	normalized := make([]domain.CreateArtifact, len(inputs))
	for index, input := range inputs {
		input = input.Normalized()
		if err := input.Validate(); err != nil {
			return BatchResult{}, domain.Invalid("items", fmt.Sprintf("item %d: %s", index, err.Error()))
		}
		if err := s.policy.CheckCreate(input); err != nil {
			return BatchResult{}, domain.Invalid("items", fmt.Sprintf("item %d: %s", index, err.Error()))
		}
		normalized[index] = input
	}

	artifacts := make([]domain.Artifact, 0, len(normalized))
	events := make([]domain.AuditEvent, 0, len(normalized))
	for _, input := range normalized {
		now := s.clock().UTC()
		artifact := domain.Artifact{
			ID:        s.nextID(),
			Title:     input.Title,
			Summary:   input.Summary,
			Source:    input.Source,
			Year:      input.Year,
			Tags:      domain.NormalizeTags(input.Tags),
			Status:    domain.StatusDraft,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := artifact.Validate(); err != nil {
			return BatchResult{}, err
		}
		before := domain.Artifact{ID: artifact.ID}
		event, err := domain.NewAuditEvent(s.nextID(), before, artifact, domain.ActionCreate, s.actor(actor), "batch intake", artifact.UpdatedAt)
		if err != nil {
			return BatchResult{}, err
		}
		artifacts = append(artifacts, artifact)
		events = append(events, event)
	}

	saved := make([]domain.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if err := s.repo.Save(ctx, artifact); err != nil {
			return BatchResult{}, errors.Join(err, s.rollbackArtifacts(ctx, saved))
		}
		saved = append(saved, artifact)
	}
	appended := make([]domain.AuditEvent, 0, len(events))
	for _, event := range events {
		if err := s.audit.Append(ctx, event); err != nil {
			rollbackErr := s.rollbackArtifacts(ctx, saved)
			for _, existing := range appended {
				if deleteErr := s.audit.DeleteByArtifact(ctx, existing.ArtifactID); deleteErr != nil {
					rollbackErr = errors.Join(rollbackErr, deleteErr)
				}
			}
			return BatchResult{}, errors.Join(err, rollbackErr)
		}
		appended = append(appended, event)
	}
	for _, artifact := range saved {
		if err := s.recordSnapshot(ctx, artifact); err != nil {
			rollbackErr := s.rollbackArtifacts(ctx, saved)
			for _, existing := range appended {
				if deleteErr := s.audit.DeleteByArtifact(ctx, existing.ArtifactID); deleteErr != nil {
					rollbackErr = errors.Join(rollbackErr, deleteErr)
				}
			}
			return BatchResult{}, errors.Join(err, rollbackErr)
		}
	}
	return BatchResult{Count: len(saved), Artifacts: saved}, nil
}

func (s *Service) rollbackArtifacts(ctx context.Context, values []domain.Artifact) error {
	var result error
	for index := len(values) - 1; index >= 0; index-- {
		if err := s.repo.Delete(ctx, values[index].ID); err != nil {
			result = errors.Join(result, err)
		}
		if err := s.audit.DeleteByArtifact(ctx, values[index].ID); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}
