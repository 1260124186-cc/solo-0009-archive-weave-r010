package domain

import (
	"strings"
	"time"
)

type Status string

const (
	StatusDraft         Status = "draft"
	StatusPendingReview Status = "pending_review"
	StatusApproved      Status = "approved"
)

type Artifact struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Source      string     `json:"source"`
	Year        int        `json:"year"`
	Tags        []Tag      `json:"tags"`
	Status      Status     `json:"status"`
	Version     int        `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
	Reviews     []Review   `json:"reviews,omitempty"`
}

type CreateArtifact struct {
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Source  string   `json:"source"`
	Year    int      `json:"year"`
	Tags    []string `json:"tags"`
}

func (a CreateArtifact) Validate() error {
	if err := ValidateText("title", a.Title, 2, 180); err != nil {
		return err
	}
	if err := ValidateText("summary", a.Summary, 12, 4000); err != nil {
		return err
	}
	if err := ValidateText("source", a.Source, 2, 220); err != nil {
		return err
	}
	if a.Year < 1000 || a.Year > time.Now().UTC().Year() {
		return Invalid("year", "year is outside supported range")
	}
	normalized := NormalizeTags(a.Tags)
	if len(normalized) == 0 {
		return Invalid("tags", "at least one tag is required")
	}
	if len(normalized) > 24 {
		return Invalid("tags", "no more than 24 tags are allowed")
	}
	for _, tag := range normalized {
		if err := ValidateText("tag", tag.Name, 1, 48); err != nil {
			return err
		}
	}
	return nil
}

func (a CreateArtifact) Normalized() CreateArtifact {
	a.Title = strings.TrimSpace(a.Title)
	a.Summary = strings.TrimSpace(a.Summary)
	a.Source = strings.TrimSpace(a.Source)
	a.Tags = TagNames(NormalizeTags(a.Tags))
	return a
}

func (a Artifact) Validate() error {
	if strings.TrimSpace(a.ID) == "" {
		return Invalid("id", "id is required")
	}
	if err := (CreateArtifact{
		Title:   a.Title,
		Summary: a.Summary,
		Source:  a.Source,
		Year:    a.Year,
		Tags:    TagNames(a.Tags),
	}).Validate(); err != nil {
		return err
	}
	if !a.Status.Valid() {
		return Invalid("status", "unknown status")
	}
	if a.Version < 1 {
		return Invalid("version", "version must be positive")
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		return Invalid("timestamps", "created and updated timestamps are required")
	}
	if a.UpdatedAt.Before(a.CreatedAt) {
		return Invalid("timestamps", "updated timestamp cannot precede creation")
	}
	if a.SubmittedAt != nil && a.SubmittedAt.Before(a.CreatedAt) {
		return Invalid("submitted_at", "submission cannot precede creation")
	}
	if a.DecidedAt != nil && a.SubmittedAt == nil {
		return Invalid("decided_at", "a decided artifact must have been submitted")
	}
	return nil
}

func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusPendingReview, StatusApproved:
		return true
	default:
		return false
	}
}

func (a Artifact) CanSubmit() bool {
	return a.Status == StatusDraft &&
		strings.TrimSpace(a.Title) != "" &&
		strings.TrimSpace(a.Summary) != "" &&
		strings.TrimSpace(a.Source) != "" &&
		len(a.Tags) > 0
}

func (a Artifact) CanReview() bool {
	return a.Status == StatusPendingReview
}

func (a Artifact) IsPublic() bool {
	return a.Status == StatusApproved
}

func (a Artifact) Touch(now time.Time) Artifact {
	a.Version++
	a.UpdatedAt = now.UTC()
	return a
}

func (a Artifact) AddReview(review Review) Artifact {
	a.Reviews = append([]Review(nil), a.Reviews...)
	a.Reviews = append(a.Reviews, review)
	return a
}

func (a Artifact) Clone() Artifact {
	a.Tags = append([]Tag(nil), a.Tags...)
	a.Reviews = append([]Review(nil), a.Reviews...)
	if a.SubmittedAt != nil {
		value := *a.SubmittedAt
		a.SubmittedAt = &value
	}
	if a.DecidedAt != nil {
		value := *a.DecidedAt
		a.DecidedAt = &value
	}
	return a
}
