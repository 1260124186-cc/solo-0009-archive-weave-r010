package workflowcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"example.com/solo-0009-archive-weave/internal/domain"
)

type comparisonSubmission struct {
	Reference domain.ComparisonTarget   `json:"reference"`
	Targets   []domain.ComparisonTarget `json:"targets"`
}

type comparisonEnvelope struct {
	Job    domain.ComparisonJob `json:"job"`
	Reused bool                 `json:"reused"`
}

// checkArchiveComparison 验证比较主流程：
// 逐项失败原因、版本固化（后续修订不改写结论）、相同输入复用、非法引用拒绝。
func checkArchiveComparison(ctx context.Context, h *harness) error {
	reference, err := h.create(ctx, "Canal charter ledger", "Ledger recording canal tolls and cargo movements", []string{"canal", "ledger"})
	if err != nil {
		return err
	}
	target, err := h.create(ctx, "Canal charter ledger copy", "Ledger recording canal tolls and cargo movements", []string{"canal", "ledger"})
	if err != nil {
		return err
	}

	// 提交：引用 + 四个目标（相等、有差异、档案不存在、版本不合法）。
	submission := comparisonSubmission{
		Reference: domain.ComparisonTarget{ArtifactID: reference.ID, Version: reference.Version},
		Targets: []domain.ComparisonTarget{
			{ArtifactID: reference.ID, Version: reference.Version},
			{ArtifactID: target.ID, Version: 0},
			{ArtifactID: "artifact-does-not-exist", Version: 0},
			{ArtifactID: reference.ID, Version: 9999},
		},
	}
	var envelope comparisonEnvelope
	if err := h.request(ctx, http.MethodPost, "/comparisons", submission, &envelope); err != nil {
		return err
	}
	if envelope.Reused {
		return fmt.Errorf("fresh comparison unexpectedly reused: %#v", envelope)
	}
	job, err := h.waitForComparison(ctx, envelope.Job.ID, 5*time.Second)
	if err != nil {
		return err
	}
	if job.Status != domain.ComparisonJobCompleted || job.CompletedItems != 2 || job.FailedItems != 2 {
		return fmt.Errorf("unexpected comparison job outcome: %#v", job)
	}
	if err := assertItemOutcomes(job); err != nil {
		return err
	}

	// 固化验证：引用档案修订升版后，已保存结论仍绑定旧版本且内容不变。
	if _, err := h.update(ctx, reference.ID, domain.CreateArtifact{
		Title:   "Canal charter ledger annotated",
		Summary: "Ledger recording canal tolls and cargo movements",
		Source:  "Reading room transfer",
		Year:    1932,
		Tags:    []string{"canal", "ledger", "annotated"},
	}); err != nil {
		return err
	}
	persisted, err := h.getComparison(ctx, envelope.Job.ID)
	if err != nil {
		return err
	}
	if err := assertItemOutcomes(persisted); err != nil {
		return fmt.Errorf("comparison conclusion changed after reference revision: %w", err)
	}

	// 复用验证：相同输入再次提交应直接返回既有任务。
	var repeat comparisonEnvelope
	if err := h.request(ctx, http.MethodPost, "/comparisons", submission, &repeat); err != nil {
		return err
	}
	if !repeat.Reused || repeat.Job.ID != envelope.Job.ID {
		return fmt.Errorf("identical input was not reused: %#v", repeat)
	}

	// 非法引用必须整体拒绝（4xx），不能产生半截任务。
	status, _, err := h.rawRequest(ctx, http.MethodPost, "/comparisons", comparisonSubmission{
		Reference: domain.ComparisonTarget{ArtifactID: "missing-reference", Version: 0},
		Targets:   []domain.ComparisonTarget{{ArtifactID: target.ID}},
	})
	if err != nil {
		return err
	}
	if status < 400 || status >= 500 {
		return fmt.Errorf("missing reference should be rejected with 4xx, got %d", status)
	}
	return nil
}

func assertItemOutcomes(job domain.ComparisonJob) error {
	byTarget := map[string]domain.ComparisonItem{}
	for _, item := range job.Items {
		byTarget[item.Target.ArtifactID+":"+strconv.Itoa(item.Target.Version)] = item
	}
	if len(byTarget) != 4 {
		return fmt.Errorf("expected 4 distinct items, got %d", len(byTarget))
	}
	for _, item := range job.Items {
		switch item.Target.ArtifactID {
		case "artifact-does-not-exist":
			if item.Status != domain.ComparisonItemFailed ||
				item.ErrorCode != domain.ComparisonReasonTargetMissing {
				return fmt.Errorf("missing target not marked correctly: %#v", item)
			}
			if item.Result != nil {
				return fmt.Errorf("failed item must not carry a result: %#v", item)
			}
		default:
			if item.Target.ArtifactID == "" {
				continue
			}
			if item.Target.Version == 9999 {
				if item.Status != domain.ComparisonItemFailed ||
					item.ErrorCode != domain.ComparisonReasonTargetVersion {
					return fmt.Errorf("invalid version not marked correctly: %#v", item)
				}
			}
		}
	}
	equal := findItemByEquality(job, true)
	if equal == nil || equal.Result == nil || !equal.Result.Equal ||
		equal.Result.ReferenceVersion != 1 || equal.Result.TargetVersion != 1 {
		return fmt.Errorf("self comparison should be equal and pinned to version 1: %#v", equal)
	}
	different := findItemByEquality(job, false)
	if different == nil || different.Result == nil || different.Result.Equal {
		return fmt.Errorf("renamed target comparison should differ: %#v", different)
	}
	foundTitleDiff := false
	for _, diff := range different.Result.Differences {
		if diff.Field == "title" && diff.Text != nil &&
			diff.Text.From == "Canal charter ledger" && diff.Text.To == "Canal charter ledger copy" {
			foundTitleDiff = true
		}
	}
	if !foundTitleDiff {
		return fmt.Errorf("expected title difference, got: %#v", different.Result.Differences)
	}
	return nil
}

func findItemByEquality(job domain.ComparisonJob, equal bool) *domain.ComparisonItem {
	for index := range job.Items {
		item := &job.Items[index]
		if item.Status != domain.ComparisonItemCompleted || item.Result == nil {
			continue
		}
		if item.Result.Equal == equal {
			return item
		}
	}
	return nil
}

func (h *harness) getComparison(ctx context.Context, id string) (domain.ComparisonJob, error) {
	var job domain.ComparisonJob
	if err := h.request(ctx, http.MethodGet, "/comparisons/"+id, nil, &job); err != nil {
		return domain.ComparisonJob{}, err
	}
	return job, nil
}

func (h *harness) waitForComparison(ctx context.Context, id string, timeout time.Duration) (domain.ComparisonJob, error) {
	deadline := time.Now().Add(timeout)
	for {
		job, err := h.getComparison(ctx, id)
		if err != nil {
			return domain.ComparisonJob{}, err
		}
		if job.Status == domain.ComparisonJobCompleted {
			return job, nil
		}
		if time.Now().After(deadline) {
			return domain.ComparisonJob{}, fmt.Errorf("comparison job %s did not complete in time: %#v", id, job)
		}
		select {
		case <-ctx.Done():
			return domain.ComparisonJob{}, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (h *harness) rawRequest(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, h.base+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("X-Archive-Actor", "workflow-check")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := h.client.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("read %s %s: %w", method, path, err)
	}
	return response.StatusCode, data, nil
}
