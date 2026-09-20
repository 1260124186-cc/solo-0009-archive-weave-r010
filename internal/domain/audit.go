package domain

import (
	"sort"
	"strings"
	"time"
)

type AuditEvent struct {
	ID         string           `json:"id"`
	ArtifactID string           `json:"artifact_id"`
	Action     TransitionAction `json:"action"`
	FromStatus Status           `json:"from_status"`
	ToStatus   Status           `json:"to_status"`
	Actor      string           `json:"actor"`
	Note       string           `json:"note,omitempty"`
	Version    int              `json:"version"`
	OccurredAt time.Time        `json:"occurred_at"`
}

func NewAuditEvent(id string, before, after Artifact, action TransitionAction, actor, note string, at time.Time) (AuditEvent, error) {
	if err := ValidateIdentifier("event_id", id); err != nil {
		return AuditEvent{}, err
	}
	if err := ValidateIdentifier("artifact_id", before.ID); err != nil {
		return AuditEvent{}, err
	}
	if err := ValidateIdentifier("actor", actor); err != nil {
		return AuditEvent{}, err
	}
	if at.IsZero() {
		return AuditEvent{}, Invalid("occurred_at", "event timestamp is required")
	}
	if after.Version < before.Version {
		return AuditEvent{}, Invalid("version", "event version cannot move backwards")
	}
	return AuditEvent{
		ID:         strings.TrimSpace(id),
		ArtifactID: before.ID,
		Action:     action,
		FromStatus: before.Status,
		ToStatus:   after.Status,
		Actor:      strings.TrimSpace(actor),
		Note:       strings.TrimSpace(note),
		Version:    after.Version,
		OccurredAt: at.UTC(),
	}, nil
}

func (e AuditEvent) Validate() error {
	if err := ValidateIdentifier("event_id", e.ID); err != nil {
		return err
	}
	if err := ValidateIdentifier("artifact_id", e.ArtifactID); err != nil {
		return err
	}
	if err := ValidateIdentifier("actor", e.Actor); err != nil {
		return err
	}
	if e.Action == "" {
		return Invalid("action", "event action is required")
	}
	if e.Version < 1 {
		return Invalid("version", "event version must be positive")
	}
	if e.OccurredAt.IsZero() {
		return Invalid("occurred_at", "event timestamp is required")
	}
	return nil
}

type Timeline struct {
	ArtifactID string       `json:"artifact_id"`
	Events     []AuditEvent `json:"events"`
}

func NewTimeline(artifactID string, events []AuditEvent) Timeline {
	ordered := append([]AuditEvent(nil), events...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Version == ordered[j].Version {
			return ordered[i].OccurredAt.Before(ordered[j].OccurredAt)
		}
		return ordered[i].Version < ordered[j].Version
	})
	return Timeline{ArtifactID: artifactID, Events: ordered}
}
