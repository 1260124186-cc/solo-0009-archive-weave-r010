package catalog

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"example.com/solo-0009-archive-weave/internal/domain"
	"example.com/solo-0009-archive-weave/internal/storage"
)

// MaximumTargets bounds a single submission so a large request cannot
// monopolise the comparison workers.
const MaximumTargets = 50

// Default comparison scheduling parameters.
const (
	defaultWorkers    = 2
	queueCapacity     = 256
	reapInterval      = 500 * time.Millisecond
	itemLeaseDuration = 30 * time.Second
)

// ComparisonInput is the use-case request for one comparison job.
type ComparisonInput struct {
	Actor     string
	Reference domain.VersionRef
	Targets   []domain.VersionRef
}

func (i ComparisonInput) Normalized() ComparisonInput {
	i.Actor = strings.TrimSpace(i.Actor)
	i.Reference = i.Reference.Normalized()
	i.Targets = append([]domain.VersionRef(nil), i.Targets...)
	for index := range i.Targets {
		i.Targets[index] = i.Targets[index].Normalized()
	}
	return i
}

func (i ComparisonInput) Validate() error {
	if strings.TrimSpace(i.Reference.ArtifactID) == "" {
		return domain.Invalid("reference", "reference artifact id is required")
	}
	if len(i.Targets) == 0 {
		return domain.Invalid("targets", "at least one target is required")
	}
	if len(i.Targets) > MaximumTargets {
		return domain.Invalid("targets", "no more than 50 targets are allowed")
	}
	for index, target := range i.Targets {
		if strings.TrimSpace(target.ArtifactID) == "" {
			return domain.Invalid("targets", "every target needs an artifact id")
		}
		if target.Version != 0 && target.Version < 1 {
			return domain.Invalid("targets", "target version must be positive or 0 for latest")
		}
		if index > 0 {
			for previous := 0; previous < index; previous++ {
				if i.Targets[previous] == target {
					return domain.Invalid("targets", "duplicate target references are not allowed")
				}
			}
		}
	}
	return nil
}

// ComparisonView is the stable read shape returned by the HTTP layer.
type ComparisonView struct {
	ID               string                  `json:"id"`
	Fingerprint      string                  `json:"fingerprint"`
	Reference        domain.VersionRef       `json:"reference"`
	ReferenceVersion int                     `json:"reference_version"`
	Status           domain.ComparisonStatus `json:"status"`
	Total            int                     `json:"total"`
	Succeeded        int                     `json:"succeeded"`
	Failed           int                     `json:"failed"`
	Pending          int                     `json:"pending"`
	Items            []domain.ComparisonItem `json:"items"`
	Actor            string                  `json:"actor"`
	Reused           bool                    `json:"reused"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
}

func viewOf(job domain.ComparisonJob) ComparisonView {
	pending := 0
	for _, item := range job.Items {
		if !item.Status.Terminal() {
			pending++
		}
	}
	return ComparisonView{
		ID:               job.ID,
		Fingerprint:      job.Fingerprint,
		Reference:        job.Reference,
		ReferenceVersion: job.ReferenceVersion,
		Status:           job.Status,
		Total:            len(job.Items),
		Succeeded:        job.SucceededCount(),
		Failed:           job.FailedCount(),
		Pending:          pending,
		Items:            append([]domain.ComparisonItem(nil), job.Items...),
		Actor:            job.Actor,
		CreatedAt:        job.CreatedAt,
		UpdatedAt:        job.UpdatedAt,
	}
}

type workItem struct {
	jobID    string
	index    int
	owner    string
	enqueued time.Time
}

// ComparisonService runs bounded, resumable version comparisons.
type ComparisonService struct {
	jobs      storage.ComparisonRepository
	artifacts storage.Repository
	snapshots storage.VersionSnapshotRepository

	workers   int
	queue     chan workItem
	nextID    func() string
	now       func() time.Time
	ownerSeed string

	onSubmit func()
	onItem   func(succeeded bool)

	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool
}

// NewComparisonService wires the comparison use case onto the existing stores.
func NewComparisonService(jobs storage.ComparisonRepository, artifacts storage.Repository, snapshots storage.VersionSnapshotRepository, workers int) *ComparisonService {
	if workers < 1 {
		workers = defaultWorkers
	}
	return &ComparisonService{
		jobs:      jobs,
		artifacts: artifacts,
		snapshots: snapshots,
		workers:   workers,
		queue:     make(chan workItem, queueCapacity),
		nextID:    uuid.NewString,
		now:       time.Now,
		ownerSeed: uuid.NewString(),
	}
}

// WithHooks attaches lightweight observability callbacks.
func (s *ComparisonService) WithHooks(onSubmit func(), onItem func(succeeded bool)) *ComparisonService {
	s.onSubmit = onSubmit
	s.onItem = onItem
	return s
}

// Start launches the worker pool and the recovery/reaper loop.
func (s *ComparisonService) Start(parent context.Context) {
	if s.started {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.started = true
	for worker := 0; worker < s.workers; worker++ {
		s.wg.Add(1)
		go s.runWorker(ctx, worker)
	}
	s.wg.Add(1)
	go s.runReaper(ctx)
	_ = s.Recover(ctx)
}

// Stop signals the workers to stop and waits for in-flight comparisons to
// finish so persisted jobs never lose a result that was computed.
func (s *ComparisonService) Stop() {
	if !s.started {
		return
	}
	s.cancel()
	s.wg.Wait()
	s.started = false
}
