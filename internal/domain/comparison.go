package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// ComparisonStatus tracks the lifecycle of an individual comparison item.
type ComparisonStatus string

const (
	ComparisonPending   ComparisonStatus = "pending"
	ComparisonRunning   ComparisonStatus = "running"
	ComparisonSucceeded ComparisonStatus = "succeeded"
	ComparisonFailed    ComparisonStatus = "failed"
)

func (s ComparisonStatus) Valid() bool {
	switch s {
	case ComparisonPending, ComparisonRunning, ComparisonSucceeded, ComparisonFailed:
		return true
	default:
		return false
	}
}

func (s ComparisonStatus) Terminal() bool {
	return s == ComparisonSucceeded || s == ComparisonFailed
}

// Failure codes preserved on individual items when their reference cannot be used.
const (
	ComparisonFailureNotFound = "artifact_not_found"
	ComparisonFailureVersion  = "invalid_version"
)

// VersionRef identifies one artifact version. Version 0 (or a negative value)
// means "the latest version at submission time".
type VersionRef struct {
	ArtifactID string `json:"artifact_id"`
	Version    int    `json:"version"`
}

func (r VersionRef) Normalized() VersionRef {
	r.ArtifactID = strings.TrimSpace(r.ArtifactID)
	return r
}

func (r VersionRef) WantsLatest() bool {
	return r.Version < 1
}

// FieldChange is one per-field result inside a comparison. Failed comparisons
// leave Changes empty.
type FieldChange struct {
	Field   string `json:"field"`
	Changed bool   `json:"changed"`
	Before  any    `json:"before,omitempty"`
	After   any    `json:"after,omitempty"`
}

type TagSetChange struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// ComparisonResult is the immutable, snapshot-bound conclusion.
type ComparisonResult struct {
	ReferenceSnapshot VersionSnapshot `json:"reference_snapshot"`
	TargetSnapshot    VersionSnapshot `json:"target_snapshot"`
	Identical         bool            `json:"identical"`
	ChangedFields     []string        `json:"changed_fields"`
	Changes           []FieldChange   `json:"changes"`
	ReferenceChecksum string          `json:"reference_checksum"`
	TargetChecksum    string          `json:"target_checksum"`
	ComputedAt        time.Time       `json:"computed_at"`
}

// ComparisonItem compares the job reference against one target version.
type ComparisonItem struct {
	Index           int               `json:"index"`
	Target          VersionRef        `json:"target"`
	ResolvedVersion int               `json:"resolved_version,omitempty"`
	Status          ComparisonStatus  `json:"status"`
	FailureCode     string            `json:"failure_code,omitempty"`
	FailureReason   string            `json:"failure_reason,omitempty"`
	Result          *ComparisonResult `json:"result,omitempty"`
	StartedAt       *time.Time        `json:"started_at,omitempty"`
	FinishedAt      *time.Time        `json:"finished_at,omitempty"`
	Attempts        int               `json:"attempts"`
	LeaseOwner      string            `json:"lease_owner,omitempty"`
	LeasedUntil     *time.Time        `json:"leased_until,omitempty"`
}

// ComparisonJob is one submission: a single reference versus many targets.
type ComparisonJob struct {
	ID                string           `json:"id"`
	Fingerprint       string           `json:"fingerprint"`
	Reference         VersionRef       `json:"reference"`
	ReferenceVersion  int              `json:"reference_version"`
	ReferenceSnapshot VersionSnapshot  `json:"reference_snapshot"`
	Actor             string           `json:"actor"`
	Status            ComparisonStatus `json:"status"`
	Items             []ComparisonItem `json:"items"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

// Validate enforces the invariants a persisted job must hold.
func (j ComparisonJob) Validate() error {
	if err := ValidateIdentifier("id", j.ID); err != nil {
		return err
	}
	if strings.TrimSpace(j.Fingerprint) == "" {
		return Invalid("fingerprint", "fingerprint is required")
	}
	if strings.TrimSpace(j.Reference.ArtifactID) == "" {
		return Invalid("reference", "reference artifact is required")
	}
	if j.ReferenceVersion < 1 {
		return Invalid("reference_version", "resolved reference version must be positive")
	}
	if err := j.ReferenceSnapshot.Validate(); err != nil {
		return err
	}
	if !j.Status.Valid() {
		return Invalid("status", "unknown comparison status")
	}
	if len(j.Items) == 0 {
		return Invalid("items", "at least one target is required")
	}
	if len(j.Items) > 50 {
		return Invalid("items", "no more than 50 targets are allowed")
	}
	seen := map[int]bool{}
	for _, item := range j.Items {
		if item.Index < 0 || seen[item.Index] {
			return Invalid("items", "item indices must be unique and non-negative")
		}
		seen[item.Index] = true
		if strings.TrimSpace(item.Target.ArtifactID) == "" {
			return Invalid("items", "every target needs an artifact id")
		}
		if !item.Status.Valid() {
			return Invalid("status", "unknown item comparison status")
		}
		if item.Status == ComparisonSucceeded && item.Result == nil {
			return Invalid("result", "succeeded item must carry a result")
		}
		if item.Status == ComparisonFailed && strings.TrimSpace(item.FailureReason) == "" {
			return Invalid("failure_reason", "failed item must carry a reason")
		}
	}
	if j.CreatedAt.IsZero() || j.UpdatedAt.IsZero() {
		return Invalid("timestamps", "created and updated timestamps are required")
	}
	if j.UpdatedAt.Before(j.CreatedAt) {
		return Invalid("timestamps", "updated timestamp cannot precede creation")
	}
	return nil
}

// Clone returns a deep copy so callers cannot mutate persisted state through a read.
func (j ComparisonJob) Clone() ComparisonJob {
	items := make([]ComparisonItem, len(j.Items))
	for index, item := range j.Items {
		items[index] = cloneItem(item)
	}
	j.Items = items
	return j
}

func cloneItem(item ComparisonItem) ComparisonItem {
	if item.StartedAt != nil {
		value := *item.StartedAt
		item.StartedAt = &value
	}
	if item.FinishedAt != nil {
		value := *item.FinishedAt
		item.FinishedAt = &value
	}
	if item.LeasedUntil != nil {
		value := *item.LeasedUntil
		item.LeasedUntil = &value
	}
	if item.Result != nil {
		result := *item.Result
		result.ChangedFields = append([]string(nil), item.Result.ChangedFields...)
		result.Changes = append([]FieldChange(nil), item.Result.Changes...)
		item.Result = &result
	}
	return item
}

// Refresh recomputes item-derived counters and the job status. The job reaches
// the terminal succeeded state only when every item has a terminal state.
func (j ComparisonJob) Refresh(now time.Time) ComparisonJob {
	pending := 0
	for _, item := range j.Items {
		if !item.Status.Terminal() {
			pending++
		}
	}
	if pending == 0 {
		j.Status = ComparisonSucceeded
	} else if j.Status == ComparisonSucceeded {
		j.Status = ComparisonPending
	}
	j.UpdatedAt = now.UTC()
	return j
}

// SucceededCount and FailedCount power the read API.
func (j ComparisonJob) SucceededCount() int {
	count := 0
	for _, item := range j.Items {
		if item.Status == ComparisonSucceeded {
			count++
		}
	}
	return count
}

func (j ComparisonJob) FailedCount() int {
	count := 0
	for _, item := range j.Items {
		if item.Status == ComparisonFailed {
			count++
		}
	}
	return count
}

// FingerprintInput is the canonical shape hashed for result reuse. The request
// fingerprint is derived from the versions actually resolved at submission
// time, so "latest" is bound to the version that was current then.
type FingerprintInput struct {
	Reference VersionRef   `json:"reference"`
	Targets   []VersionRef `json:"targets"`
}

// ComparisonFingerprint hashes the resolved reference and targets.
func ComparisonFingerprint(reference VersionRef, targets []VersionRef) string {
	ordered := make([]VersionRef, len(targets))
	copy(ordered, targets)
	sort.Slice(ordered, func(i, k int) bool {
		if ordered[i].ArtifactID != ordered[k].ArtifactID {
			return ordered[i].ArtifactID < ordered[k].ArtifactID
		}
		return ordered[i].Version < ordered[k].Version
	})
	payload, _ := json.Marshal(FingerprintInput{Reference: reference, Targets: ordered})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

// ArtifactChecksum hashes the canonical JSON of one artifact version.
func ArtifactChecksum(artifact Artifact) string {
	payload, err := json.Marshal(artifact)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

// CompareSnapshots produces the immutable field-by-field result between the
// reference version and one target version.
func CompareSnapshots(reference, target VersionSnapshot, now time.Time) ComparisonResult {
	changes := []FieldChange{
		{
			Field:   "title",
			Changed: strings.TrimSpace(reference.Artifact.Title) != strings.TrimSpace(target.Artifact.Title),
			Before:  reference.Artifact.Title,
			After:   target.Artifact.Title,
		},
		{
			Field:   "summary",
			Changed: strings.TrimSpace(reference.Artifact.Summary) != strings.TrimSpace(target.Artifact.Summary),
			Before:  reference.Artifact.Summary,
			After:   target.Artifact.Summary,
		},
		{
			Field:   "source",
			Changed: strings.TrimSpace(reference.Artifact.Source) != strings.TrimSpace(target.Artifact.Source),
			Before:  reference.Artifact.Source,
			After:   target.Artifact.Source,
		},
		{
			Field:   "year",
			Changed: reference.Artifact.Year != target.Artifact.Year,
			Before:  reference.Artifact.Year,
			After:   target.Artifact.Year,
		},
		{
			Field:   "status",
			Changed: reference.Artifact.Status != target.Artifact.Status,
			Before:  string(reference.Artifact.Status),
			After:   string(target.Artifact.Status),
		},
	}
	referenceTags := TagNames(reference.Artifact.Tags)
	targetTags := TagNames(target.Artifact.Tags)
	added, removed := diffTagSets(referenceTags, targetTags)
	tagChanged := len(added) > 0 || len(removed) > 0
	changes = append(changes, FieldChange{
		Field:   "tags",
		Changed: tagChanged,
		Before:  referenceTags,
		After:   targetTags,
	})
	changedFields := make([]string, 0)
	for _, change := range changes {
		if change.Changed {
			changedFields = append(changedFields, change.Field)
		}
	}
	identical := len(changedFields) == 0
	return ComparisonResult{
		ReferenceSnapshot: reference,
		TargetSnapshot:    target,
		Identical:         identical,
		ChangedFields:     changedFields,
		Changes:           changes,
		ReferenceChecksum: ArtifactChecksum(reference.Artifact),
		TargetChecksum:    ArtifactChecksum(target.Artifact),
		ComputedAt:        now.UTC(),
	}
}

func diffTagSets(reference, target []string) (added []string, removed []string) {
	referenceSet := map[string]bool{}
	for _, tag := range reference {
		referenceSet[tag] = true
	}
	targetSet := map[string]bool{}
	for _, tag := range target {
		targetSet[tag] = true
	}
	added = make([]string, 0)
	removed = make([]string, 0)
	for _, tag := range target {
		if !referenceSet[tag] {
			added = append(added, tag)
		}
	}
	for _, tag := range reference {
		if !targetSet[tag] {
			removed = append(removed, tag)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// TagChangeDetail extracts the added/removed detail for the tags change. It
// returns nil when tags did not change.
func (r ComparisonResult) TagChangeDetail() *TagSetChange {
	if r.ReferenceSnapshot.Artifact.ID == "" {
		return nil
	}
	added, removed := diffTagSets(
		TagNames(r.ReferenceSnapshot.Artifact.Tags),
		TagNames(r.TargetSnapshot.Artifact.Tags),
	)
	if len(added) == 0 && len(removed) == 0 {
		return nil
	}
	return &TagSetChange{Added: added, Removed: removed}
}
