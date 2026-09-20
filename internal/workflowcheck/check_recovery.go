package workflowcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"time"

	"example.com/solo-0009-archive-weave/internal/catalog"
	"example.com/solo-0009-archive-weave/internal/comparison"
	"example.com/solo-0009-archive-weave/internal/domain"
	"example.com/solo-0009-archive-weave/internal/httpapi"
	"example.com/solo-0009-archive-weave/internal/observability"
	"example.com/solo-0009-archive-weave/internal/storage"
)

// comparisonBarrier 阻塞在途比较项目，用于制造未结束任务并观测并发。
type comparisonBarrier struct {
	entered atomic.Int32
	release chan struct{}
	started chan struct{}
}

// checkComparisonRecovery 验证：
// 1. 有界并发——提交大批目标时同时在途数量不超过配置上限；
// 2. 重启恢复——任务在途时"杀掉"进程，新进程从磁盘恢复并完成全部项目；
// 3. 重启后已保存结论仍可查看。
func checkComparisonRecovery(ctx context.Context, h *harness) error {
	directory := h.dir

	// 准备引用与多个目标档案。
	reference, err := h.create(ctx, "Harbor ledger", "Ledger describing harbor dues and berth allocations", []string{"harbor", "ledger"})
	if err != nil {
		return err
	}
	targetIDs := make([]string, 0, 6)
	for index := 0; index < 6; index++ {
		artifact, err := h.create(ctx,
			fmt.Sprintf("Harbor ledger variant %d", index),
			"Ledger describing harbor dues and berth allocations",
			[]string{"harbor", "ledger"})
		if err != nil {
			return err
		}
		targetIDs = append(targetIDs, artifact.ID)
	}

	barrier := &comparisonBarrier{
		release: make(chan struct{}),
		started: make(chan struct{}, 8),
	}
	inFlight := atomic.Int32{}
	maxObserved := atomic.Int32{}

	// 第一个进程：并发上限为 2，且每个项目在比较前进入屏障，任务因此停留在途。
	manager, server := startRecoveryInstance(directory, 2, &inFlight, &maxObserved, barrier)
	firstClient := &http.Client{Timeout: 5 * time.Second}

	targets := make([]domain.ComparisonTarget, 0, len(targetIDs))
	for _, id := range targetIDs {
		targets = append(targets, domain.ComparisonTarget{ArtifactID: id})
	}
	submission := comparisonSubmission{
		Reference: domain.ComparisonTarget{ArtifactID: reference.ID},
		Targets:   targets,
	}
	var envelope comparisonEnvelope
	if err := postJSON(ctx, firstClient, server.URL+"/comparisons", submission, &envelope); err != nil {
		return err
	}
	jobID := envelope.Job.ID

	// 等待两个项目进入屏障，证明并发上限生效。
	if err := waitForBarrier(ctx, barrier.started, 2, 3*time.Second); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	if max := maxObserved.Load(); max > 2 {
		return fmt.Errorf("comparison concurrency exceeded limit: observed %d", max)
	}

	// 优雅停机：先停 HTTP，再释放屏障让在途项目"来不及"完成——关闭时限给得很短，
	// 在途项目应保持 running 落盘，由下一实例恢复，而不是被误标为失败。
	server.Close()
	close(barrier.release)
	closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := manager.Close(closeCtx); err == nil {
		// 时限极短，通常应超时返回；即便恰好完成也允许，后续仍能读到已完成任务。
	}

	// 第二个进程：从同一磁盘恢复，不再设置屏障，应完成全部项目。
	recoveryClient := &http.Client{Timeout: 5 * time.Second}
	recoveryBase, stopRecovery := startRecoveryServer(directory)
	defer stopRecovery()

	recovered, err := pollComparison(ctx, recoveryClient, recoveryBase, jobID, 5*time.Second)
	if err != nil {
		return err
	}
	if recovered.Status != domain.ComparisonJobCompleted {
		return fmt.Errorf("recovered job did not complete: %#v", recovered)
	}
	if recovered.CompletedItems != len(targets) || recovered.FailedItems != 0 {
		return fmt.Errorf("recovered job item counts mismatch: %#v", recovered)
	}
	for _, item := range recovered.Items {
		if item.Status != domain.ComparisonItemCompleted || item.Result == nil {
			return fmt.Errorf("recovered item not completed with result: %#v", item)
		}
		if item.Result.ReferenceVersion != 1 {
			return fmt.Errorf("recovered item not pinned to reference v1: %#v", item)
		}
	}
	return nil
}

func startRecoveryInstance(
	directory string,
	concurrency int,
	inFlight, maxObserved *atomic.Int32,
	barrier *comparisonBarrier,
) (*comparison.Manager, *httptest.Server) {
	snapshots := storage.NewJSONSnapshotStore(filepath.Join(directory, "snapshots.json"))
	repository := storage.NewSnapshottingRepository(
		storage.NewJSONStore(filepath.Join(directory, "artifacts.json")), snapshots)
	auditRepository := storage.NewJSONAuditStore(filepath.Join(directory, "history.json"))
	service := catalog.NewService(repository, auditRepository)
	jobStore := comparison.NewJSONJobStore(filepath.Join(directory, "comparisons.json"))
	manager := comparison.NewManager(jobStore, snapshots, comparison.Options{
		Concurrency:    concurrency,
		RescanInterval: 100 * time.Millisecond,
		BeforeCompare: func(ctx context.Context, _ string, _ domain.ComparisonItem) {
			current := inFlight.Add(1)
			for {
				recorded := maxObserved.Load()
				if current <= recorded || maxObserved.CompareAndSwap(recorded, current) {
					break
				}
			}
			barrier.started <- struct{}{}
			if barrier != nil {
				select {
				case <-barrier.release:
				case <-ctx.Done():
				}
			}
			inFlight.Add(-1)
		},
	}, observability.NewLogger(), &observability.Metrics{})
	manager.Start(context.Background())
	server := httptest.NewServer(httpapi.NewServer(service, manager,
		observability.NewLogger(), &observability.Metrics{}, false).Handler())
	return manager, server
}

func startRecoveryServer(directory string) (string, func()) {
	snapshots := storage.NewJSONSnapshotStore(filepath.Join(directory, "snapshots.json"))
	repository := storage.NewSnapshottingRepository(
		storage.NewJSONStore(filepath.Join(directory, "artifacts.json")), snapshots)
	auditRepository := storage.NewJSONAuditStore(filepath.Join(directory, "history.json"))
	service := catalog.NewService(repository, auditRepository)
	jobStore := comparison.NewJSONJobStore(filepath.Join(directory, "comparisons.json"))
	manager := comparison.NewManager(jobStore, snapshots, comparison.Options{
		Concurrency:    2,
		RescanInterval: 100 * time.Millisecond,
	}, observability.NewLogger(), &observability.Metrics{})
	manager.Start(context.Background())
	server := httptest.NewServer(httpapi.NewServer(service, manager,
		observability.NewLogger(), &observability.Metrics{}, false).Handler())
	return server.URL, server.Close
}

func waitForBarrier(ctx context.Context, started chan struct{}, wanted int, timeout time.Duration) error {
	deadline := time.After(timeout)
	seen := 0
	for seen < wanted {
		select {
		case <-started:
			seen++
		case <-deadline:
			return fmt.Errorf("only %d comparison items started, want %d", seen, wanted)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func postJSON(ctx context.Context, client *http.Client, url string, body any, target any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request body: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("post %s: %w", url, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("read post response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("post %s returned %d: %s", url, response.StatusCode, string(data))
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode post response: %w", err)
	}
	return nil
}

func pollComparison(
	ctx context.Context,
	client *http.Client,
	baseURL, jobID string,
	timeout time.Duration,
) (domain.ComparisonJob, error) {
	deadline := time.Now().Add(timeout)
	url := baseURL + "/comparisons/" + jobID
	var last domain.ComparisonJob
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return domain.ComparisonJob{}, err
		}
		response, err := client.Do(request)
		if err == nil {
			data, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
			_ = response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK {
				if err := json.Unmarshal(data, &last); err == nil {
					if last.Status == domain.ComparisonJobCompleted {
						return last, nil
					}
				}
			}
		}
		if time.Now().After(deadline) {
			return domain.ComparisonJob{}, fmt.Errorf("comparison %s not recovered in time: %#v", jobID, last)
		}
		select {
		case <-ctx.Done():
			return domain.ComparisonJob{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
