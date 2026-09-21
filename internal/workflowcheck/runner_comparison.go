package workflowcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"example.com/solo-0009-archive-weave/internal/catalog"
	"example.com/solo-0009-archive-weave/internal/domain"
)

type comparisonRequestBody struct {
	Actor     string                    `json:"actor,omitempty"`
	Reference comparisonRequestTarget   `json:"reference"`
	Targets   []comparisonRequestTarget `json:"targets"`
}

type comparisonRequestTarget struct {
	ArtifactID string `json:"artifact_id"`
	Version    int    `json:"version,omitempty"`
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Field   string `json:"field"`
	} `json:"error"`
}

func checkCompareVersions(ctx context.Context, h *harness) error {
	reference, err := h.create(ctx, "Deed box 12", "Canonical deed used as the comparison reference", []string{"deed", "reference"})
	if err != nil {
		return err
	}
	target, err := h.create(ctx, "Deed box 12", "Canonical deed used as the comparison reference", []string{"deed", "reference"})
	if err != nil {
		return err
	}
	missingID := uuid.NewString()

	// 1. One reference against: an identical target, a missing artifact and an
	// illegal version. The bad items must keep their reasons while the valid
	// item completes.
	first, status, reused, err := h.submitComparison(ctx, comparisonRequestBody{
		Reference: comparisonRequestTarget{ArtifactID: reference.ID},
		Targets: []comparisonRequestTarget{
			{ArtifactID: target.ID},
			{ArtifactID: missingID},
			{ArtifactID: target.ID, Version: 99},
		},
	})
	if err != nil {
		return err
	}
	if status != http.StatusCreated || reused {
		return fmt.Errorf("unexpected comparison submit status=%d reused=%v", status, reused)
	}
	done, err := h.comparison.WaitForJob(ctx, first.ID, 5*time.Second)
	if err != nil {
		return err
	}
	if done.Status != domain.ComparisonSucceeded || done.Total != 3 || done.Succeeded != 1 || done.Failed != 2 {
		return fmt.Errorf("unexpected comparison totals: %#v", done)
	}
	if err := assertItemOutcome(done.Items, 0, domain.ComparisonSucceeded, ""); err != nil {
		return err
	}
	if err := assertItemOutcome(done.Items, 1, domain.ComparisonFailed, domain.ComparisonFailureNotFound); err != nil {
		return err
	}
	if err := assertItemOutcome(done.Items, 2, domain.ComparisonFailed, domain.ComparisonFailureVersion); err != nil {
		return err
	}
	result := done.Items[0].Result
	if result == nil || !result.Identical || len(result.ChangedFields) != 0 {
		return fmt.Errorf("identical target was not reported identical: %#v", result)
	}
	if result.ReferenceChecksum == "" || result.TargetChecksum == "" {
		return fmt.Errorf("comparison result is missing checksums: %#v", result)
	}

	// 2. Identical input reuses the saved job even if it is submitted again.
	again, _, reusedAgain, err := h.submitComparison(ctx, comparisonRequestBody{
		Reference: comparisonRequestTarget{ArtifactID: reference.ID},
		Targets: []comparisonRequestTarget{
			{ArtifactID: target.ID},
			{ArtifactID: missingID},
			{ArtifactID: target.ID, Version: 99},
		},
	})
	if err != nil {
		return err
	}
	if !reusedAgain || again.ID != first.ID {
		return fmt.Errorf("identical input was not reused: got id=%s want %s", again.ID, first.ID)
	}

	// 3. A missing reference rejects the whole submission; it is never turned
	// into a per-item failure.
	_, refStatus, _, refErr := h.submitComparison(ctx, comparisonRequestBody{
		Reference: comparisonRequestTarget{ArtifactID: missingID},
		Targets:   []comparisonRequestTarget{{ArtifactID: target.ID}},
	})
	if refErr == nil || refStatus != http.StatusNotFound {
		return fmt.Errorf("missing reference status=%d err=%v, want 404", refStatus, refErr)
	}

	// 4. Revise the target to v2, then ask for the pinned v1 comparison again.
	// The saved conclusion must remain identical even though the artifact moved.
	revised, err := h.update(ctx, target.ID, domain.CreateArtifact{
		Title:   "Deed box 12 revised edition",
		Summary: "Canonical deed used as the comparison reference",
		Source:  "Reading room transfer",
		Year:    1932,
		Tags:    []string{"deed", "reference", "revised"},
	})
	if err != nil {
		return err
	}
	if revised.Version != 2 {
		return fmt.Errorf("target revision did not advance to v2: %#v", revised)
	}
	saved, err := h.comparison.GetComparison(ctx, first.ID)
	if err != nil {
		return err
	}
	if saved.Items[0].Result == nil || !saved.Items[0].Result.Identical {
		return fmt.Errorf("saved v1 conclusion was rewritten by later revision: %#v", saved.Items[0].Result)
	}

	// Historical v1 is still resolvable from the pinned snapshot.
	historical, _, _, err := h.submitComparison(ctx, comparisonRequestBody{
		Reference: comparisonRequestTarget{ArtifactID: reference.ID, Version: 1},
		Targets:   []comparisonRequestTarget{{ArtifactID: target.ID, Version: 1}},
	})
	if err != nil {
		return err
	}
	historicalDone, err := h.comparison.WaitForJob(ctx, historical.ID, 5*time.Second)
	if err != nil {
		return err
	}
	if historicalDone.Failed != 0 || !historicalDone.Items[0].Result.Identical {
		return fmt.Errorf("pinned historical comparison failed after revision: %#v", historicalDone)
	}

	// Latest target is v2 now and must show title and tag differences.
	latest, _, _, err := h.submitComparison(ctx, comparisonRequestBody{
		Reference: comparisonRequestTarget{ArtifactID: reference.ID},
		Targets:   []comparisonRequestTarget{{ArtifactID: target.ID}},
	})
	if err != nil {
		return err
	}
	latestDone, err := h.comparison.WaitForJob(ctx, latest.ID, 5*time.Second)
	if err != nil {
		return err
	}
	latestResult := latestDone.Items[0].Result
	if latestResult == nil || latestResult.Identical {
		return fmt.Errorf("latest comparison should detect the revision: %#v", latestResult)
	}
	if !contains(latestResult.ChangedFields, "title") || !contains(latestResult.ChangedFields, "tags") {
		return fmt.Errorf("revision differences were not reported: %#v", latestResult.ChangedFields)
	}

	// 5. The request limit rejects oversized batches before scheduling work.
	tooMany := make([]comparisonRequestTarget, 51)
	for index := range tooMany {
		tooMany[index] = comparisonRequestTarget{ArtifactID: fmt.Sprintf("artifact-%d", index)}
	}
	_, overStatus, _, overErr := h.submitComparison(ctx, comparisonRequestBody{
		Reference: comparisonRequestTarget{ArtifactID: reference.ID},
		Targets:   tooMany,
	})
	if overErr == nil || overStatus != http.StatusBadRequest {
		return fmt.Errorf("oversized batch status=%d err=%v, want 400", overStatus, overErr)
	}

	// 6. A bounded batch of many distinct targets drains under the small worker
	// pool without losing items.
	items := make([]domain.CreateArtifact, 0, 20)
	for index := 0; index < 20; index++ {
		items = append(items, domain.CreateArtifact{
			Title:   fmt.Sprintf("Flood plain register volume %d", index),
			Summary: "Register volume describing flood plain field measurements",
			Source:  "Reading room transfer",
			Year:    1932,
			Tags:    []string{"register", fmt.Sprintf("volume-%d", index)},
		})
	}
	batch, err := h.importBatch(ctx, items)
	if err != nil {
		return err
	}
	targets := make([]comparisonRequestTarget, 0, len(batch.Artifacts))
	for _, artifact := range batch.Artifacts {
		targets = append(targets, comparisonRequestTarget{ArtifactID: artifact.ID})
	}
	volume, _, _, err := h.submitComparison(ctx, comparisonRequestBody{
		Reference: comparisonRequestTarget{ArtifactID: reference.ID},
		Targets:   targets,
	})
	if err != nil {
		return err
	}
	volumeDone, err := h.comparison.WaitForJob(ctx, volume.ID, 10*time.Second)
	if err != nil {
		return err
	}
	if volumeDone.Total != 20 || volumeDone.Succeeded+volumeDone.Failed != 20 {
		return fmt.Errorf("bounded batch did not drain completely: %#v", volumeDone)
	}

	// 7. Simulate a crash: persist a job with an item stuck in "running", then
	// start a fresh comparison service over the same files. Recovery must
	// finish it without touching the already terminal items.
	stuckID := "stuck-restart-job"
	referenceSnapshot, err := h.snapshots.GetVersion(ctx, reference.ID, reference.Version)
	if err != nil {
		return err
	}
	startedAt := time.Now().UTC().Add(-time.Hour)
	leasedUntil := time.Now().UTC().Add(-time.Hour)
	stuck := domain.ComparisonJob{
		ID:                stuckID,
		Fingerprint:       "stuck-fingerprint",
		Reference:         domain.VersionRef{ArtifactID: reference.ID, Version: 1},
		ReferenceVersion:  1,
		ReferenceSnapshot: referenceSnapshot,
		Actor:             "workflow-check",
		Status:            domain.ComparisonPending,
		Items: []domain.ComparisonItem{
			{
				Index:           0,
				Target:          domain.VersionRef{ArtifactID: target.ID, Version: 1},
				ResolvedVersion: 1,
				Status:          domain.ComparisonRunning,
				StartedAt:       &startedAt,
				LeasedUntil:     &leasedUntil,
			},
			{
				Index:         1,
				Target:        domain.VersionRef{ArtifactID: missingID, Version: 1},
				Status:        domain.ComparisonFailed,
				FailureCode:   domain.ComparisonFailureNotFound,
				FailureReason: "not_found: artifact not found",
				FinishedAt:    &startedAt,
			},
		},
		CreatedAt: startedAt,
		UpdatedAt: startedAt,
	}
	if err := h.jobs.CreateJob(ctx, stuck); err != nil {
		return err
	}
	if err := h.restartComparison(ctx); err != nil {
		return err
	}
	recovered, err := h.comparison.WaitForJob(ctx, stuckID, 10*time.Second)
	if err != nil {
		return err
	}
	if recovered.Status != domain.ComparisonSucceeded || recovered.Succeeded != 1 || recovered.Failed != 1 {
		return fmt.Errorf("recovered job did not finish as expected: %#v", recovered)
	}
	if recovered.Items[0].Result == nil || recovered.Items[0].Result.TargetSnapshot.Version != 1 {
		return fmt.Errorf("recovered item did not bind to the pinned target version: %#v", recovered.Items[0])
	}
	return nil
}

func assertItemOutcome(items []domain.ComparisonItem, index int, status domain.ComparisonStatus, code string) error {
	if len(items) <= index {
		return fmt.Errorf("comparison is missing item %d", index)
	}
	item := items[index]
	if item.Status != status {
		return fmt.Errorf("item %d status=%s, want %s", index, item.Status, status)
	}
	if status == domain.ComparisonFailed {
		if item.FailureCode != code || strings.TrimSpace(item.FailureReason) == "" {
			return fmt.Errorf("item %d failure code=%q reason=%q, want code=%q with a reason", index, item.FailureCode, item.FailureReason, code)
		}
	}
	return nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (h *harness) submitComparison(ctx context.Context, body comparisonRequestBody) (catalog.ComparisonView, int, bool, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return catalog.ComparisonView{}, 0, false, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, h.base+"/comparisons", bytes.NewReader(payload))
	if err != nil {
		return catalog.ComparisonView{}, 0, false, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Archive-Actor", "workflow-check")
	response, err := h.client.Do(request)
	if err != nil {
		return catalog.ComparisonView{}, 0, false, err
	}
	defer response.Body.Close()
	data, err := readLimited(response)
	if err != nil {
		return catalog.ComparisonView{}, response.StatusCode, false, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var decoded errorBody
		_ = json.Unmarshal(data, &decoded)
		return catalog.ComparisonView{}, response.StatusCode, false,
			fmt.Errorf("POST /comparisons returned %d: %s %s", response.StatusCode, decoded.Error.Code, decoded.Error.Message)
	}
	var view catalog.ComparisonView
	if err := json.Unmarshal(data, &view); err != nil {
		return catalog.ComparisonView{}, response.StatusCode, false, err
	}
	return view, response.StatusCode, response.Header.Get("X-Comparison-Reused") == "true", nil
}

func (h *harness) restartComparison(ctx context.Context) error {
	h.comparison.Stop()
	service := catalog.NewComparisonService(h.jobs, h.repo, h.snapshots, 2)
	runCtx, cancel := context.WithCancel(ctx)
	h.cancel()
	h.cancel = cancel
	service.Start(runCtx)
	h.comparison = service
	return nil
}
