package domain

import "time"

// VersionSnapshot captures an artifact version at the moment it was written.
// Comparations bind to these snapshots so later revisions can never rewrite a
// conclusion that has already been persisted.
type VersionSnapshot struct {
	ArtifactID string    `json:"artifact_id"`
	Version    int       `json:"version"`
	Artifact   Artifact  `json:"artifact"`
	CapturedAt time.Time `json:"captured_at"`
}

func (s VersionSnapshot) Validate() error {
	if err := ValidateIdentifier("artifact_id", s.ArtifactID); err != nil {
		return err
	}
	if s.Version < 1 {
		return Invalid("version", "snapshot version must be positive")
	}
	if err := s.Artifact.Validate(); err != nil {
		return err
	}
	if s.Artifact.ID != s.ArtifactID {
		return Invalid("artifact_id", "snapshot artifact identity mismatch")
	}
	if s.Artifact.Version != s.Version {
		return Invalid("version", "snapshot artifact version mismatch")
	}
	if s.CapturedAt.IsZero() {
		return Invalid("captured_at", "snapshot capture time is required")
	}
	return nil
}
