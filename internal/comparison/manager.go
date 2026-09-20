package comparison

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"example.com/solo-0009-archive-weave/internal/domain"
	"example.com/solo-0009-archive-weave/internal/observability"
	"example.com/solo-0009-archive-weave/internal/storage"
)

const (
	defaultConcurrency = 4
	// rescanInterval 是分发器在提交通道空闲时重扫持久化积压的周期，
	// 用于兜底恢复调度中错过或服务重启后仍处于 pending/running 的项目。
	defaultRescanInterval = 500 * time.Millisecond
	// enqueueBuffer 限制等待进入调度的任务数，避免提交洪峰占满内存。
	enqueueBuffer = 64
	// listLimit 是默认返回的任务条数上限。
	listLimit = 100
)

// Options 控制比较管理器的运行参数。
type Options struct {
	Concurrency    int
	RescanInterval time.Duration
	Clock          func() time.Time
	NextID         func() string
	// BeforeCompare 在每个项目比较前执行，测试用以注入延迟验证并发上限。
	BeforeCompare func(ctx context.Context, jobID string, item domain.ComparisonItem)
}

// Stats 暴露管理器运行状态。
type Stats struct {
	Concurrency    int `json:"concurrency"`
	InFlight       int `json:"in_flight"`
	QueuedSignals  int `json:"queued_signals"`
	SubmittedJobs  int `json:"submitted_jobs"`
	ReusedJobs     int `json:"reused_jobs"`
	CompletedItems int `json:"completed_items"`
	FailedItems    int `json:"failed_items"`
	RecoveredItems int `json:"recovered_items"`
}

// Manager 编排比较任务：提交、去重复用、有界并发执行、持久化与重启恢复。
type Manager struct {
	store    JobStore
	resolver *VersionResolver
	opts     Options
	logger   *observability.Logger
	metrics  *observability.Metrics

	enqueue    chan string
	wake       chan struct{}
	workers    chan struct{}
	inFlight   int
	inFlightMu sync.Mutex
	wg         sync.WaitGroup

	// workerCtx 与 worker 生命周期一致：仅在 Close 时取消，不随服务启动上下文结束，
	// 使优雅停机期间在途项目可以选择完成或保持 running 等待恢复，而非被误记为失败。
	workerCtx   context.Context
	stopWorkers context.CancelFunc

	// activeItems 记录本进程内真正在执行的项目 ID，用于区分上次进程残留的 running。
	activeMu    sync.Mutex
	activeItems map[string]struct{}

	statsMu    sync.Mutex
	statsValue Stats

	startOnce sync.Once
	closeOnce sync.Once
	done      chan struct{}
	stopped   chan struct{}
}

// NewManager 创建比较管理器。Start 之前提交的任务会在 Start 后被重扫恢复。
func NewManager(store JobStore, snapshots storage.SnapshotRepository, opts Options,
	logger *observability.Logger, metrics *observability.Metrics) *Manager {
	if opts.Concurrency <= 0 {
		opts.Concurrency = defaultConcurrency
	}
	if opts.RescanInterval <= 0 {
		opts.RescanInterval = defaultRescanInterval
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.NextID == nil {
		opts.NextID = uuid.NewString
	}
	if logger == nil {
		logger = observability.NewLogger()
	}
	if metrics == nil {
		metrics = &observability.Metrics{}
	}
	workerContext, stopWorkers := context.WithCancel(context.Background())
	return &Manager{
		store:       store,
		resolver:    NewVersionResolver(snapshots),
		opts:        opts,
		logger:      logger,
		metrics:     metrics,
		enqueue:     make(chan string, enqueueBuffer),
		wake:        make(chan struct{}, 1),
		workers:     make(chan struct{}, opts.Concurrency),
		workerCtx:   workerContext,
		stopWorkers: stopWorkers,
		done:        make(chan struct{}),
		stopped:     make(chan struct{}),
		activeItems: map[string]struct{}{},
		statsValue:  Stats{Concurrency: opts.Concurrency},
	}
}

// Start 启动分发器与恢复扫描，可重复调用且仅生效一次。
func (m *Manager) Start(ctx context.Context) {
	m.startOnce.Do(func() {
		go m.dispatchLoop(ctx)
		go m.rescanLoop(ctx)
	})
}

// Close 停止接收新调度。它先尝试在给定时限内等待在途比较自然完成；
// 若超时则取消 worker 上下文，在途项目保持 running 状态留在磁盘上，
// 由下次启动时的恢复扫描重新调度，不会被错误标记为失败。
func (m *Manager) Close(ctx context.Context) error {
	m.closeOnce.Do(func() { close(m.done) })
	finished := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		<-m.stopped
		m.stopWorkers()
		return nil
	case <-ctx.Done():
		m.stopWorkers()
		<-m.stopped
		return ctx.Err()
	}
}

// Submit 规范化输入、固化引用与全部目标的具体版本并创建比较任务。
// 若相同（引用ID+版本、目标ID+版本 全部固化后）的任务已存在，则直接复用既有结果。
// 引用不存在或版本不合法时整体拒绝；目标版本问题在任务中逐项预置为失败，
// 其余可解析目标进入有界并发执行。
func (m *Manager) Submit(ctx context.Context, request domain.ComparisonRequest) (domain.ComparisonJob, error) {
	normalized, err := NormalizeRequest(request)
	if err != nil {
		return domain.ComparisonJob{}, err
	}

	// 引用必须在提交时可解析；解析后固化具体版本号，之后修订不影响本任务。
	resolvedReference, err := m.resolver.Resolve(ctx, normalized.Reference)
	if err != nil {
		return domain.ComparisonJob{}, mapResolutionError("reference", normalized.Reference, err)
	}
	boundReference := domain.ComparisonTarget{
		ArtifactID: normalized.Reference.ArtifactID,
		Version:    resolvedReference.Version,
	}

	now := m.opts.Clock().UTC()
	items := make([]domain.ComparisonItem, 0, len(normalized.Targets))
	boundTargets := make([]domain.ComparisonTarget, 0, len(normalized.Targets))
	for _, target := range normalized.Targets {
		boundTarget := target
		item := domain.ComparisonItem{
			ID:        m.opts.NextID(),
			Reference: boundReference,
			Target:    target,
			Status:    domain.ComparisonItemPending,
		}
		// 同步解析并固化目标版本；单个目标非法只影响该项目。
		resolvedTarget, resolveErr := m.resolver.Resolve(ctx, target)
		if resolveErr != nil {
			finished := now
			item.Status = domain.ComparisonItemFailed
			item.ErrorCode = failureCode("target", target, resolveErr)
			item.Error = resolveErr.Error()
			item.StartedAt = &finished
			item.FinishedAt = &finished
			m.bumpFailed()
		} else {
			boundTarget = domain.ComparisonTarget{
				ArtifactID: target.ArtifactID,
				Version:    resolvedTarget.Version,
			}
			item.Target = boundTarget
		}
		boundTargets = append(boundTargets, boundTarget)
		items = append(items, item)
	}

	// 指纹基于全部固化后的具体版本，使显式版本与"当时最新"解析出的同版本互相复用。
	boundRequest := domain.ComparisonRequest{
		Actor:     normalized.Actor,
		Reference: boundReference,
		Targets:   boundTargets,
	}
	fingerprint := Fingerprint(boundRequest)
	if existing, found, err := m.store.FindFingerprint(ctx, fingerprint); err != nil {
		return domain.ComparisonJob{}, err
	} else if found {
		m.bumpReused()
		reused := cloneJob(existing)
		reused.Reused = true
		return reused, nil
	}

	job := domain.ComparisonJob{
		ID:          m.opts.NextID(),
		Fingerprint: fingerprint,
		Actor:       normalized.Actor,
		Status:      domain.ComparisonJobQueued,
		Reference:   boundReference,
		Items:       items,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	recomputeJob(&job, now)
	if err := job.Validate(); err != nil {
		return domain.ComparisonJob{}, err
	}
	if err := m.store.Save(ctx, job); err != nil {
		return domain.ComparisonJob{}, err
	}
	m.bumpSubmitted()
	m.signal(job.ID)
	return cloneJob(job), nil
}

// Get 返回比较任务（含逐项结果与失败原因）。
func (m *Manager) Get(ctx context.Context, id string) (domain.ComparisonJob, error) {
	return m.store.Get(ctx, id)
}

// List 返回最近的比较任务。
func (m *Manager) List(ctx context.Context, limit int) ([]domain.ComparisonJob, error) {
	if limit <= 0 || limit > listLimit {
		limit = listLimit
	}
	return m.store.List(ctx, limit)
}

// Stats 返回当前运行计数快照。
func (m *Manager) Stats() Stats {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	stats := m.statsValue
	stats.InFlight = m.inFlightCount()
	stats.QueuedSignals = len(m.enqueue)
	return stats
}

func (m *Manager) signal(jobID string) {
	select {
	case m.enqueue <- jobID:
	default:
		// 提交通道饱和时不阻塞提交者，重扫循环会兜底捡起该任务。
		m.wakeDispatcher()
	}
}

func (m *Manager) wakeDispatcher() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) dispatchLoop(startCtx context.Context) {
	defer close(m.stopped)
	for {
		select {
		case <-m.done:
			// 停止新调度；在途项目由 Close 通过 WaitGroup 跟踪，workerCtx
			// 独立于本循环，故正在执行的项目不会因分发器退出而被取消。
			return
		case <-startCtx.Done():
			// 启动上下文结束不代表服务关停：保留在途项目，仅停止调度，
			// 它们仍可由新的 Start 后的重扫循环发现（通常不会走到这里）。
			return
		case jobID := <-m.enqueue:
			m.scheduleJob(m.workerCtx, jobID)
		case <-m.wake:
			m.scanAndSchedule(m.workerCtx, 1)
		}
	}
}

// scheduleJob 为一个任务中所有可运行项目申请 worker 令牌并启动。
func (m *Manager) scheduleJob(ctx context.Context, jobID string) {
	job, err := m.store.Get(ctx, jobID)
	if err != nil {
		var appError domain.AppError
		if errors.As(err, &appError) && appError.Code == domain.ErrNotFound {
			return
		}
		m.logger.Event("comparison_schedule_error", map[string]any{"job_id": jobID, "error": err.Error()})
		return
	}
	m.launchPending(ctx, &job)
}

// scanAndSchedule 扫描持久化任务，为仍有积压的任务补充调度。
func (m *Manager) scanAndSchedule(ctx context.Context, maxJobs int) {
	jobs, err := m.store.List(ctx, listLimit)
	if err != nil {
		m.logger.Event("comparison_rescan_error", map[string]any{"error": err.Error()})
		return
	}
	scanned := 0
	for index := range jobs {
		if scanned >= maxJobs {
			return
		}
		job := jobs[index]
		if job.Status == domain.ComparisonJobCompleted {
			continue
		}
		if hasOpenItems(job) {
			recovered := m.resetStaleRunning(ctx, &job)
			if recovered > 0 {
				m.bumpRecovered(recovered)
			}
			if hasOpenItems(job) {
				m.launchPending(ctx, &job)
				scanned++
			}
		}
	}
}

// resetStaleRunning 把不属于本进程活跃集合的 running 项目退回 pending，
// 使服务重启后崩溃在途的比较能够被重新调度。
func (m *Manager) resetStaleRunning(ctx context.Context, job *domain.ComparisonJob) int {
	recovered := 0
	changed := false
	for index := range job.Items {
		item := &job.Items[index]
		if item.Status != domain.ComparisonItemRunning {
			continue
		}
		if m.isActive(item.ID) {
			continue
		}
		item.Status = domain.ComparisonItemPending
		item.StartedAt = nil
		changed = true
		recovered++
	}
	if changed {
		recomputeJob(job, m.opts.Clock().UTC())
		if err := m.store.Save(ctx, *job); err != nil {
			m.logger.Event("comparison_recover_error", map[string]any{"job_id": job.ID, "error": err.Error()})
			return 0
		}
	}
	return recovered
}

// launchPending 在不超过并发上限的前提下为 pending 项目启动 goroutine。
func (m *Manager) launchPending(ctx context.Context, job *domain.ComparisonJob) {
	for index := range job.Items {
		item := &job.Items[index]
		if item.Status != domain.ComparisonItemPending {
			continue
		}
		select {
		case m.workers <- struct{}{}:
		default:
			// 并发已达上限：剩余项目保持 pending，由后续唤醒或重扫继续调度。
			return
		}
		m.markInFlight(1)
		started := m.opts.Clock().UTC()
		item.Status = domain.ComparisonItemRunning
		item.StartedAt = &started
		runningItem := cloneItem(*item)
		runningJobID := job.ID
		if err := m.store.Save(ctx, *job); err != nil {
			m.logger.Event("comparison_state_error", map[string]any{"job_id": runningJobID, "error": err.Error()})
			<-m.workers
			m.markInFlight(-1)
			// 状态未落盘则该项目仍为 pending，交给重扫恢复。
			return
		}
		m.setActive(item.ID)
		m.wg.Add(1)
		go func(item domain.ComparisonItem) {
			defer m.wg.Done()
			m.runItem(ctx, runningJobID, item)
		}(runningItem)
	}
}

// runItem 执行单个比较项目。任何解析或比较失败都落到该项目上，不影响兄弟项目。
func (m *Manager) runItem(ctx context.Context, jobID string, item domain.ComparisonItem) {
	defer func() {
		<-m.workers
		m.markInFlight(-1)
		m.clearActive(item.ID)
	}()

	if m.opts.BeforeCompare != nil {
		m.opts.BeforeCompare(ctx, jobID, item)
	}

	status := domain.ComparisonItemCompleted
	errorCode := ""
	errorMessage := ""
	var result *domain.ComparisonItemResult

	reference, err := m.resolver.Resolve(ctx, item.Reference)
	if err != nil {
		if m.abortForShutdown(ctx, err) {
			return
		}
		status = domain.ComparisonItemFailed
		errorCode = failureCode("reference", item.Reference, err)
		errorMessage = err.Error()
	} else {
		targetResolved, err := m.resolver.Resolve(ctx, item.Target)
		if err != nil {
			if m.abortForShutdown(ctx, err) {
				return
			}
			status = domain.ComparisonItemFailed
			errorCode = failureCode("target", item.Target, err)
			errorMessage = err.Error()
		} else {
			// 比较只针对冻结快照，结论不随后续档案修订而变化。
			comparison := CompareArtifacts(reference.Artifact, targetResolved.Artifact)
			result = &comparison
		}
	}
	if status == domain.ComparisonItemCompleted {
		m.bumpCompleted()
	} else {
		m.bumpFailed()
	}

	// CAS 式更新：重读最新任务、只改本项目，直到保存成功，避免覆盖兄弟项目结果。
	// 关停时仍允许把已算出的结论落盘（此时使用一个短的脱离生命周期的上下文）。
	persistCtx := ctx
	if ctx.Err() != nil {
		return
	}
	if err := m.updateItem(persistCtx, jobID, item.ID, func(target *domain.ComparisonItem, now func() time.Time) {
		target.Status = status
		target.ErrorCode = errorCode
		target.Error = errorMessage
		target.Result = result
		finishedAt := now().UTC()
		target.FinishedAt = &finishedAt
	}); err != nil {
		m.logger.Event("comparison_item_persist_error", map[string]any{
			"job_id": jobID, "item_id": item.ID, "error": err.Error(),
		})
	}
	m.wakeDispatcher()
}

// abortForShutdown 判断错误是否源于关停取消。若是则保持项目 running 状态，
// 不落任何终态，交由下次启动时的恢复扫描重新调度。
func (m *Manager) abortForShutdown(ctx context.Context, err error) bool {
	if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return true
	}
	return false
}

// updateItem 以重读-修改-重算-保存的方式更新单个项目，冲突时重试。
func (m *Manager) updateItem(
	ctx context.Context,
	jobID, itemID string,
	mutate func(item *domain.ComparisonItem, now func() time.Time),
) error {
	const maxAttempts = 8
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		job, err := m.store.Get(ctx, jobID)
		if err != nil {
			return err
		}
		for index := range job.Items {
			if job.Items[index].ID != itemID {
				continue
			}
			mutate(&job.Items[index], m.opts.Clock)
			recomputeJob(&job, m.opts.Clock().UTC())
			if err := m.store.Save(ctx, job); err != nil {
				return err
			}
			return nil
		}
		return domain.NotFound("comparison item not found")
	}
	return domain.State("comparison item update exceeded retry attempts")
}

func (m *Manager) rescanLoop(ctx context.Context) {
	// 启动后立即重扫一次，恢复上次进程未结束、持久化在磁盘上的项目。
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-ctx.Done():
			return
		case <-timer.C:
			m.scanAndSchedule(m.workerCtx, listLimit)
			timer.Reset(m.opts.RescanInterval)
		}
	}
}

func hasOpenItems(job domain.ComparisonJob) bool {
	for _, item := range job.Items {
		if item.Status == domain.ComparisonItemPending || item.Status == domain.ComparisonItemRunning {
			return true
		}
	}
	return false
}

// recomputeJob 根据逐项状态重算任务聚合计数与整体状态。
func recomputeJob(job *domain.ComparisonJob, now time.Time) {
	completed, failed, pending, running := 0, 0, 0, 0
	for _, item := range job.Items {
		switch item.Status {
		case domain.ComparisonItemCompleted:
			completed++
		case domain.ComparisonItemFailed:
			failed++
		case domain.ComparisonItemRunning:
			running++
		default:
			pending++
		}
	}
	job.CompletedItems = completed
	job.FailedItems = failed
	job.PendingItems = pending + running
	job.UpdatedAt = now.UTC()
	switch {
	case pending+running+completed+failed == 0:
		job.Status = domain.ComparisonJobQueued
	case pending+running > 0:
		if completed+failed > 0 || running > 0 {
			job.Status = domain.ComparisonJobRunning
		} else {
			job.Status = domain.ComparisonJobQueued
		}
		if job.StartedAt == nil && running > 0 {
			started := now.UTC()
			job.StartedAt = &started
		}
	default:
		job.Status = domain.ComparisonJobCompleted
		finished := now.UTC()
		job.FinishedAt = &finished
	}
}

func mapResolutionError(scope string, target domain.ComparisonTarget, err error) error {
	switch err.(type) {
	case MissingArtifactError:
		return domain.AppError{
			Code:    domain.ErrNotFound,
			Field:   scope,
			Message: err.Error(),
		}
	case InvalidVersionError:
		return domain.AppError{
			Code:    domain.ErrInvalidInput,
			Field:   scope + ".version",
			Message: err.Error(),
		}
	default:
		return err
	}
}

func failureCode(scope string, target domain.ComparisonTarget, err error) string {
	var missing MissingArtifactError
	var invalid InvalidVersionError
	switch {
	case errors.As(err, &missing):
		if scope == "reference" {
			return domain.ComparisonReasonReferenceMissing
		}
		return domain.ComparisonReasonTargetMissing
	case errors.As(err, &invalid):
		if scope == "reference" {
			return domain.ComparisonReasonReferenceVersion
		}
		return domain.ComparisonReasonTargetVersion
	default:
		return "comparison_failed"
	}
}

func (m *Manager) markInFlight(delta int) {
	m.inFlightMu.Lock()
	m.inFlight += delta
	if m.inFlight < 0 {
		m.inFlight = 0
	}
	m.inFlightMu.Unlock()
}

func (m *Manager) inFlightCount() int {
	m.inFlightMu.Lock()
	defer m.inFlightMu.Unlock()
	return m.inFlight
}

func (m *Manager) bumpSubmitted() {
	m.statsMu.Lock()
	m.statsValue.SubmittedJobs++
	m.statsMu.Unlock()
}

func (m *Manager) bumpReused() {
	m.statsMu.Lock()
	m.statsValue.ReusedJobs++
	m.statsMu.Unlock()
}

func (m *Manager) bumpCompleted() {
	m.statsMu.Lock()
	m.statsValue.CompletedItems++
	m.statsMu.Unlock()
}

func (m *Manager) bumpFailed() {
	m.statsMu.Lock()
	m.statsValue.FailedItems++
	m.statsMu.Unlock()
}

func (m *Manager) bumpRecovered(count int) {
	m.statsMu.Lock()
	m.statsValue.RecoveredItems += count
	m.statsMu.Unlock()
}

func (m *Manager) setActive(itemID string) {
	m.activeMu.Lock()
	m.activeItems[itemID] = struct{}{}
	m.activeMu.Unlock()
}

func (m *Manager) clearActive(itemID string) {
	m.activeMu.Lock()
	delete(m.activeItems, itemID)
	m.activeMu.Unlock()
}

func (m *Manager) isActive(itemID string) bool {
	m.activeMu.Lock()
	defer m.activeMu.Unlock()
	_, ok := m.activeItems[itemID]
	return ok
}
