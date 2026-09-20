package domain

import (
	"strings"
	"time"
)

type Decision string

const (
	Approve Decision = "approve"
	Return  Decision = "return"
)

type Review struct {
	ID         string    `json:"id"`
	ArtifactID string    `json:"artifact_id"`
	Decision   Decision  `json:"decision"`
	Reviewer   string    `json:"reviewer"`
	Note       string    `json:"note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func ParseDecision(value string) (Decision, error) {
	decision := Decision(strings.ToLower(strings.TrimSpace(value)))
	if decision != Approve && decision != Return {
		return "", Invalid("decision", "must be approve or return")
	}
	return decision, nil
}

func (r Review) Validate() error {
	if err := ValidateIdentifier("review_id", r.ID); err != nil {
		return err
	}
	if err := ValidateIdentifier("artifact_id", r.ArtifactID); err != nil {
		return err
	}
	if err := ValidateIdentifier("reviewer", r.Reviewer); err != nil {
		return err
	}
	if r.Decision != Approve && r.Decision != Return {
		return Invalid("decision", "must be approve or return")
	}
	if r.Decision == Return {
		if err := ValidateText("note", r.Note, 2, 1000); err != nil {
			return err
		}
	}
	if r.CreatedAt.IsZero() {
		return Invalid("created_at", "review timestamp is required")
	}
	return nil
}

func (r Review) IsApproval() bool {
	return r.Decision == Approve
}

func (r Review) IsReturn() bool {
	return r.Decision == Return
}
