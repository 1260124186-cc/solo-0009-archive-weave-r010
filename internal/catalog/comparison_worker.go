package catalog

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"example.com/solo-0009-archive-weave/internal/domain"
)

var workerSequence atomic.Uint64

func (s *ComparisonService) runWorker(ctx context.Context, worker int) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-s.queue:
			s.processItem(ctx, worker, item)
		}
	}
}

func (s *ComparisonService) workerOwner(worker int) string {
	return fmt.Sprintf("%s-%d-%d", s.ownerSeed, worker, workerSequence.Add(1))
}

// enqueuePending offers every unfinished item to the bounded queue without
// blocking. Items that do not fit are picked up by the periodic reaper.
func (s *ComparisonService) enqueuePending(job domain.ComparisonJob) {
	now := s.now().UTC()
	for _, item := range job.Items {
		if item.Status.Terminal() {
			continue
		}
		if item.LeasedUntil != nil && item.LeasedUntil.After(now) {
			continue
		}
		work := workItem{jobID: job.ID, index: item.Index, enqueued: now}
		select {
		case s.queue <- work:
		default:
		}
	}
}

func (s *ComparisonService) runReaper(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(reapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

// sweep re-offers unfinished work: expired leases from crashed workers and
// items that never fit into the bounded queue.
func (s *ComparisonService) sweep(ctx context.Context) {
	jobs, err := s.jobs.ListJobs(ctx)
	if err != nil {
		return
	}
	now := s.now().UTC()
	for _, job := range jobs {
		if job.Status.Terminal() {
			continue
		}
		for _, item := range job.Items {
			if item.Status.Terminal() {
				continue
			}
			if item.LeasedUntil != nil && item.LeasedUntil.After(now) {
				continue
			}
			select {
			case s.queue <- workItem{jobID: job.ID, index: item.Index, enqueued: now}:
			default:
			}
		}
	}
}

func (s *ComparisonService) processItem(parent context.Context, worker int, work workItem) {
	owner := s.workerOwner(worker)
	now := s.now().UTC()
	leaseUntil := now.Add(itemLeaseDuration)

	// Claim the item with a lease under the store's atomic update. This is the
	// concurrency bound: only one worker can hold a given item.
	claimed, err := s.jobs.ModifyJob(parent, work.jobID, func(job domain.ComparisonJob) (domain.ComparisonJob, bool, error) {
		if work.index < 0 || work.index >= len(job.Items) {
			return job, false, nil
		}
		item := &job.Items[work.index]
		if item.Status.Terminal() {
			return job, false, nil
		}
		if item.LeasedUntil != nil && item.LeasedUntil.After(now) {
			return job, false, nil
		}
		item.Status = domain.ComparisonRunning
		item.LeaseOwner = owner
		item.LeasedUntil = &leaseUntil
		item.StartedAt = &now
		item.Attempts++
		return job.Refresh(now), true, nil
	})
	if err != nil {
		return
	}

	var item domain.ComparisonItem
	for _, candidate := range claimed.Items {
		if candidate.Index == work.index {
			item = candidate
		}
	}
	if item.Status != domain.ComparisonRunning || item.LeaseOwner != owner {
		return
	}

	// Item computation is local: both versions were pinned at submission time,
	// so artifact revisions can never change the outcome.
	result, computeErr := s.computeItem(parent, claimed, item)

	finishedAt := s.now().UTC()
	_, err = s.jobs.ModifyJob(parent, work.jobID, func(job domain.ComparisonJob) (domain.ComparisonJob, bool, error) {
		target := &job.Items[work.index]
		if target.Status.Terminal() {
			return job, false, nil
		}
		if target.LeaseOwner != owner {
			return job, false, nil
		}
		if computeErr != nil {
			target.Status = domain.ComparisonFailed
			target.FailureCode = "comparison_failed"
			target.FailureReason = computeErr.Error()
		} else {
			target.Status = domain.ComparisonSucceeded
			target.Result = result
		}
		target.FinishedAt = &finishedAt
		target.LeasedUntil = nil
		target.LeaseOwner = ""
		return job.Refresh(finishedAt), true, nil
	})
	if err != nil {
		return
	}
	if s.onItem != nil {
		s.onItem(computeErr == nil)
	}
}

// computeItem compares the embedded reference snapshot against the target
// version. The target snapshot is resolved once more here and embedded into
// the immutable result.
func (s *ComparisonService) computeItem(ctx context.Context, job domain.ComparisonJob, item domain.ComparisonItem) (*domain.ComparisonResult, error) {
	version := item.ResolvedVersion
	if version < 1 {
		version = item.Target.Version
	}
	if version < 1 {
		return nil, errors.New("target has no resolvable version")
	}
	snapshot, err := s.snapshots.GetVersion(ctx, item.Target.ArtifactID, version)
	if err != nil {
		return nil, err
	}
	result := domain.CompareSnapshots(job.ReferenceSnapshot, snapshot, s.now())
	return &result, nil
}
