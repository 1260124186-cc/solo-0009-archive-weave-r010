package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"example.com/solo-0009-archive-weave/internal/domain"
)

// SubmitComparison creates a job for one reference versus many targets, or
// reuses an existing job with the same resolved input fingerprint.
func (s *ComparisonService) SubmitComparison(ctx context.Context, input ComparisonInput) (ComparisonView, bool, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return ComparisonView{}, false, err
	}
	actor := input.Actor
	if actor == "" {
		actor = DefaultActor
	}

	referenceRef, referenceSnapshot, err := s.resolveRef(ctx, input.Reference)
	if err != nil {
		return ComparisonView{}, false, err
	}

	resolvedTargets := make([]domain.VersionRef, len(input.Targets))
	items := make([]domain.ComparisonItem, len(input.Targets))
	for index, requested := range input.Targets {
		item := domain.ComparisonItem{Index: index, Target: requested, Status: domain.ComparisonPending}
		targetRef, snapshot, err := s.resolveRef(ctx, requested)
		if err != nil {
			item.Status = domain.ComparisonFailed
			item.FailureCode, item.FailureReason = classifyResolveFailure(err)
			item.FinishedAt = timePointer(s.now().UTC())
			resolvedTargets[index] = requested
		} else {
			item.ResolvedVersion = targetRef.Version
			resolvedTargets[index] = targetRef
			_ = snapshot // success does not embed the target until it is processed
		}
		items[index] = item
	}

	fingerprint := domain.ComparisonFingerprint(referenceRef, resolvedTargets)
	if existing, err := s.jobs.FindByFingerprint(ctx, fingerprint); err == nil {
		view := viewOf(existing)
		view.Reused = true
		return view, true, nil
	} else {
		var appError domain.AppError
		if !errors.As(err, &appError) || appError.Code != domain.ErrNotFound {
			return ComparisonView{}, false, err
		}
		// not-found is expected: there is no reusable result yet
	}

	now := s.now().UTC()
	job := domain.ComparisonJob{
		ID:                s.nextID(),
		Fingerprint:       fingerprint,
		Reference:         referenceRef,
		ReferenceVersion:  referenceRef.Version,
		ReferenceSnapshot: referenceSnapshot,
		Actor:             actor,
		Status:            domain.ComparisonPending,
		Items:             items,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	job = job.Refresh(now)
	if err := s.jobs.CreateJob(ctx, job); err != nil {
		return ComparisonView{}, false, err
	}
	if s.onSubmit != nil {
		s.onSubmit()
	}
	if job.Status != domain.ComparisonSucceeded {
		s.enqueuePending(job)
	}
	return viewOf(job), false, nil
}

// GetComparison reads one job by id.
func (s *ComparisonService) GetComparison(ctx context.Context, id string) (ComparisonView, error) {
	job, err := s.jobs.GetJob(ctx, strings.TrimSpace(id))
	if err != nil {
		return ComparisonView{}, err
	}
	return viewOf(job), nil
}

// ListComparisons returns all jobs in stable creation order.
func (s *ComparisonService) ListComparisons(ctx context.Context) ([]ComparisonView, error) {
	jobs, err := s.jobs.ListJobs(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]ComparisonView, 0, len(jobs))
	for _, job := range jobs {
		views = append(views, viewOf(job))
	}
	return views, nil
}

// ResumeComparison resets stuck items of one job back to pending and queues
// them. Already-terminal items are left untouched, so saved conclusions are
// never recomputed.
func (s *ComparisonService) ResumeComparison(ctx context.Context, id string) (ComparisonView, error) {
	job, err := s.jobs.ModifyJob(ctx, strings.TrimSpace(id), func(job domain.ComparisonJob) (domain.ComparisonJob, bool, error) {
		changed := false
		for index := range job.Items {
			if !job.Items[index].Status.Terminal() {
				job.Items[index].Status = domain.ComparisonPending
				job.Items[index].StartedAt = nil
				job.Items[index].LeasedUntil = nil
				job.Items[index].LeaseOwner = ""
				changed = true
			}
		}
		if !changed {
			return job, false, nil
		}
		return job.Refresh(s.now().UTC()), true, nil
	})
	if err != nil {
		return ComparisonView{}, err
	}
	s.enqueuePending(job)
	return viewOf(job), nil
}

// Recover re-queues every unfinished item found in durable storage. It is safe
// to run on every startup and after a queue backlog; the lease claim makes
// double processing impossible.
func (s *ComparisonService) Recover(ctx context.Context) error {
	jobs, err := s.jobs.ListJobs(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.Status.Terminal() {
			continue
		}
		if _, err := s.ResumeComparison(ctx, job.ID); err != nil {
			return err
		}
	}
	return nil
}

// WaitForJob polls until the job reaches a terminal state. It exists for the
// local workflow checks and bounded callers; production reads use GET.
func (s *ComparisonService) WaitForJob(ctx context.Context, id string, timeout time.Duration) (ComparisonView, error) {
	deadline := s.now().Add(timeout)
	for {
		view, err := s.GetComparison(ctx, id)
		if err != nil {
			return ComparisonView{}, err
		}
		if view.Status.Terminal() {
			return view, nil
		}
		if !s.now().Before(deadline) {
			return ComparisonView{}, fmt.Errorf("comparison job %s did not finish within %s", id, timeout)
		}
		select {
		case <-ctx.Done():
			return ComparisonView{}, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// resolveRef returns the concrete reference and its pinned snapshot. Version 0
// means the latest version at this moment.
func (s *ComparisonService) resolveRef(ctx context.Context, ref domain.VersionRef) (domain.VersionRef, domain.VersionSnapshot, error) {
	artifact, err := s.artifacts.Get(ctx, ref.ArtifactID)
	if err != nil {
		return domain.VersionRef{}, domain.VersionSnapshot{}, err
	}
	version := ref.Version
	if ref.WantsLatest() {
		version = artifact.Version
	}
	if version < 1 || version > artifact.Version {
		return domain.VersionRef{}, domain.VersionSnapshot{}, domain.Invalid("version",
			fmt.Sprintf("version %d is not available for artifact %s (latest %d)", version, ref.ArtifactID, artifact.Version))
	}
	snapshot, err := s.snapshots.GetVersion(ctx, ref.ArtifactID, version)
	if err != nil {
		var appError domain.AppError
		if errors.As(err, &appError) && appError.Code == domain.ErrNotFound {
			// The artifact currently carries exactly this version but no
			// snapshot was captured (e.g. pre-feature data): pin a snapshot
			// from the live artifact so comparisons stay immutable even if the
			// artifact is revised between submission and processing.
			if artifact.Version != version {
				return domain.VersionRef{}, domain.VersionSnapshot{}, domain.Invalid("version",
					fmt.Sprintf("historical version %d of artifact %s is not available", version, ref.ArtifactID))
			}
			snapshot = domain.VersionSnapshot{
				ArtifactID: artifact.ID,
				Version:    artifact.Version,
				Artifact:   artifact.Clone(),
				CapturedAt: s.now().UTC(),
			}
			if saveErr := s.snapshots.SaveVersion(ctx, snapshot); saveErr != nil {
				return domain.VersionRef{}, domain.VersionSnapshot{}, saveErr
			}
		} else {
			return domain.VersionRef{}, domain.VersionSnapshot{}, err
		}
	}
	return domain.VersionRef{ArtifactID: ref.ArtifactID, Version: version}, snapshot, nil
}

func classifyResolveFailure(err error) (code string, reason string) {
	var appError domain.AppError
	if errors.As(err, &appError) {
		switch appError.Code {
		case domain.ErrNotFound:
			return domain.ComparisonFailureNotFound, err.Error()
		case domain.ErrInvalidInput:
			return domain.ComparisonFailureVersion, err.Error()
		}
	}
	return "reference_unavailable", err.Error()
}

func timePointer(value time.Time) *time.Time {
	return &value
}
