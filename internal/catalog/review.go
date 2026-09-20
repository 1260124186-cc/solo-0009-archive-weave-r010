package catalog

import (
	"context"
	"strings"

	"example.com/solo-0009-archive-weave/internal/domain"
)

func (s *Service) Review(ctx context.Context, id, decision, reviewer, note string) (domain.Artifact, error) {
	parsed, err := domain.ParseDecision(decision)
	if err != nil {
		return domain.Artifact{}, err
	}
	reviewer = strings.TrimSpace(reviewer)
	if err := domain.ValidateIdentifier("reviewer", reviewer); err != nil {
		return domain.Artifact{}, err
	}
	if parsed == domain.Return {
		if err := domain.ValidateText("note", note, 2, 1000); err != nil {
			return domain.Artifact{}, err
		}
	}
	action := domain.ActionApprove
	if parsed == domain.Return {
		action = domain.ActionReturn
	}
	return s.applyMutation(ctx, id, action, reviewer, note, func(artifact domain.Artifact) (domain.Artifact, error) {
		now := s.clock().UTC()
		review := domain.Review{
			ID:         s.nextID(),
			ArtifactID: artifact.ID,
			Decision:   parsed,
			Reviewer:   reviewer,
			Note:       strings.TrimSpace(note),
			CreatedAt:  now,
		}
		if err := review.Validate(); err != nil {
			return domain.Artifact{}, err
		}
		updated, err := domain.ApplyTransition(artifact, domain.TransitionRequest{Action: action, Now: now})
		if err != nil {
			return domain.Artifact{}, err
		}
		updated.Reviews = append(append([]domain.Review(nil), updated.Reviews...), review)
		return updated, nil
	})
}

func (s *Service) LatestReview(artifact domain.Artifact) (domain.Review, bool) {
	if len(artifact.Reviews) == 0 {
		return domain.Review{}, false
	}
	return artifact.Reviews[len(artifact.Reviews)-1], true
}

func ReviewSummary(artifact domain.Artifact) string {
	if len(artifact.Reviews) == 0 {
		return "no review"
	}
	review := artifact.Reviews[len(artifact.Reviews)-1]
	return string(review.Decision) + " by " + review.Reviewer
}
