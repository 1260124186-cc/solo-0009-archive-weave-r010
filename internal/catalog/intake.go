package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"example.com/solo-0009-archive-weave/internal/domain"
)

func (s *Service) Create(ctx context.Context, input domain.CreateArtifact) (domain.Artifact, error) {
	return s.CreateAs(ctx, input, "")
}

func (s *Service) CreateAs(ctx context.Context, input domain.CreateArtifact, actor string) (domain.Artifact, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return domain.Artifact{}, err
	}
	if err := s.policy.CheckCreate(input); err != nil {
		return domain.Artifact{}, err
	}
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
		return domain.Artifact{}, err
	}
	if err := s.repo.Save(ctx, artifact); err != nil {
		return domain.Artifact{}, err
	}
	before := domain.Artifact{ID: artifact.ID}
	event, err := domain.NewAuditEvent(s.nextID(), before, artifact, domain.ActionCreate, s.actor(actor), "", artifact.UpdatedAt)
	if err == nil {
		err = s.audit.Append(ctx, event)
	}
	if err == nil {
		err = s.recordSnapshot(ctx, artifact)
	}
	if err != nil {
		rollbackErr := s.repo.Delete(ctx, artifact.ID)
		if rollbackErr != nil {
			return domain.Artifact{}, errors.Join(err, fmt.Errorf("rollback artifact: %w", rollbackErr))
		}
		return domain.Artifact{}, err
	}
	return artifact, nil
}

func (s *Service) UpdateMetadata(ctx context.Context, id string, input domain.CreateArtifact, actor string) (domain.Artifact, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return domain.Artifact{}, err
	}
	if err := s.policy.CheckCreate(input); err != nil {
		return domain.Artifact{}, err
	}
	metadata := domain.BuildMetadata(input)
	return s.applyMutation(ctx, id, domain.ActionUpdateMetadata, actor, "", func(artifact domain.Artifact) (domain.Artifact, error) {
		updated, err := domain.ApplyTransition(artifact, domain.TransitionRequest{Action: domain.ActionUpdateMetadata, Now: s.clock()})
		if err != nil {
			return domain.Artifact{}, err
		}
		return metadata.ApplyTo(updated), nil
	})
}

func (s *Service) Submit(ctx context.Context, id string, actor string) (domain.Artifact, error) {
	return s.applyMutation(ctx, id, domain.ActionSubmitReview, actor, "", func(artifact domain.Artifact) (domain.Artifact, error) {
		return domain.ApplyTransition(artifact, domain.TransitionRequest{Action: domain.ActionSubmitReview, Now: s.clock()})
	})
}

func (s *Service) Get(ctx context.Context, id string) (domain.Artifact, error) {
	id = strings.TrimSpace(id)
	if err := domain.ValidateIdentifier("id", id); err != nil {
		return domain.Artifact{}, err
	}
	return s.repo.Get(ctx, id)
}
