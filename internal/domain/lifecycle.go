package domain

import "time"

type TransitionAction string

const (
	ActionCreate         TransitionAction = "create"
	ActionUpdateMetadata TransitionAction = "update_metadata"
	ActionSubmitReview   TransitionAction = "submit_review"
	ActionApprove        TransitionAction = "approve"
	ActionReturn         TransitionAction = "return"
)

type TransitionRequest struct {
	Action TransitionAction
	Now    time.Time
}

func ApplyTransition(artifact Artifact, request TransitionRequest) (Artifact, error) {
	artifact = artifact.Clone()
	if request.Now.IsZero() {
		request.Now = time.Now().UTC()
	}
	now := request.Now.UTC()
	switch request.Action {
	case ActionUpdateMetadata:
		if artifact.Status != StatusDraft {
			return Artifact{}, State("only drafts can be edited")
		}
		artifact = artifact.Touch(now)
		return artifact, nil
	case ActionSubmitReview:
		if artifact.Status != StatusDraft {
			return Artifact{}, State("artifact is not a draft")
		}
		if !artifact.CanSubmit() {
			return Artifact{}, State("artifact is not complete enough for review")
		}
		artifact.Status = StatusPendingReview
		artifact.SubmittedAt = &now
		artifact.DecidedAt = nil
		artifact = artifact.Touch(now)
		return artifact, nil
	case ActionApprove:
		if artifact.Status != StatusPendingReview {
			return Artifact{}, State("artifact is not awaiting review")
		}
		artifact.Status = StatusApproved
		artifact.DecidedAt = &now
		artifact = artifact.Touch(now)
		return artifact, nil
	case ActionReturn:
		if artifact.Status != StatusPendingReview {
			return Artifact{}, State("artifact is not awaiting review")
		}
		artifact.Status = StatusDraft
		artifact.DecidedAt = &now
		artifact = artifact.Touch(now)
		return artifact, nil
	default:
		return Artifact{}, Invalid("action", "unsupported lifecycle action")
	}
}

func TransitionLabel(action TransitionAction) string {
	switch action {
	case ActionCreate:
		return "created"
	case ActionUpdateMetadata:
		return "metadata updated"
	case ActionSubmitReview:
		return "submitted for review"
	case ActionApprove:
		return "approved"
	case ActionReturn:
		return "returned to draft"
	default:
		return string(action)
	}
}
