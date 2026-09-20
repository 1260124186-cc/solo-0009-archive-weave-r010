package workflowcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"example.com/solo-0009-archive-weave/internal/catalog"
	"example.com/solo-0009-archive-weave/internal/comparison"
	"example.com/solo-0009-archive-weave/internal/domain"
	"example.com/solo-0009-archive-weave/internal/httpapi"
	"example.com/solo-0009-archive-weave/internal/observability"
	"example.com/solo-0009-archive-weave/internal/storage"
)

type harness struct {
	client  *http.Client
	base    string
	dir     string
	catalog *catalog.Service
}

func Run(ctx context.Context, workflow string) error {
	check := strings.TrimSpace(workflow)
	switch check {
	case "intake-artifact":
		return runWithHarness(ctx, verifyIntake)
	case "update-metadata":
		return runWithHarness(ctx, checkUpdateMetadata)
	case "submit-review":
		return runWithHarness(ctx, checkSubmitReview)
	case "decide-review":
		return runWithHarness(ctx, checkDecideReview)
	case "search-export":
		return runWithHarness(ctx, checkSearchExport)
	case "import-batch":
		return runWithHarness(ctx, checkImportBatch)
	case "archive-comparison":
		return runWithHarness(ctx, checkArchiveComparison)
	case "comparison-recovery":
		return runWithHarness(ctx, checkComparisonRecovery)
	case "all":
		for _, name := range []string{
			"intake-artifact",
			"update-metadata",
			"submit-review",
			"decide-review",
			"search-export",
			"import-batch",
			"archive-comparison",
			"comparison-recovery",
		} {
			if err := Run(ctx, name); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
}

func runWithHarness(ctx context.Context, check func(context.Context, *harness) error) error {
	directory, err := os.MkdirTemp("", "archive-weave-check-")
	if err != nil {
		return fmt.Errorf("create check directory: %w", err)
	}
	defer os.RemoveAll(directory)

	h, server, err := newHarness(directory)
	if err != nil {
		return err
	}
	defer server.Close()
	return check(ctx, h)
}

func newHarness(directory string) (*harness, *httptest.Server, error) {
	snapshots := storage.NewJSONSnapshotStore(filepath.Join(directory, "snapshots.json"))
	repository := storage.NewSnapshottingRepository(
		storage.NewJSONStore(filepath.Join(directory, "artifacts.json")), snapshots)
	auditRepository := storage.NewJSONAuditStore(filepath.Join(directory, "history.json"))
	service := catalog.NewService(repository, auditRepository)
	jobStore := comparison.NewJSONJobStore(filepath.Join(directory, "comparisons.json"))
	manager := comparison.NewManager(jobStore, snapshots, comparison.Options{
		Concurrency: 2,
	}, observability.NewLogger(), &observability.Metrics{})
	manager.Start(context.Background())
	server := httptest.NewServer(httpapi.NewServer(service, manager,
		observability.NewLogger(), &observability.Metrics{}, false).Handler())
	h := &harness{
		client:  &http.Client{Timeout: 10 * time.Second},
		base:    server.URL,
		dir:     directory,
		catalog: service,
	}
	return h, server, nil
}

func verifyIntake(ctx context.Context, h *harness) error {
	artifact, err := h.create(ctx, "Floodplain field notes", "Field notebook from the lower river basin", []string{"river-basin", "field-record"})
	if err != nil {
		return err
	}
	if artifact.Status != domain.StatusDraft || artifact.Version != 1 {
		return fmt.Errorf("unexpected created artifact: %#v", artifact)
	}
	fetched, err := h.getArtifact(ctx, artifact.ID)
	if err != nil {
		return err
	}
	if fetched.Title != artifact.Title {
		return fmt.Errorf("stored title mismatch: %q", fetched.Title)
	}
	timeline, err := h.history(ctx, artifact.ID)
	if err != nil {
		return err
	}
	if len(timeline.Events) != 1 || timeline.Events[0].Action != domain.ActionCreate {
		return fmt.Errorf("unexpected intake timeline: %#v", timeline)
	}
	return nil
}

func checkUpdateMetadata(ctx context.Context, h *harness) error {
	artifact, err := h.create(ctx, "Paper register fragment", "Register fragment with household notes and marginal remarks", []string{"register", "household"})
	if err != nil {
		return err
	}
	updated, err := h.update(ctx, artifact.ID, domain.CreateArtifact{
		Title:   "Revised paper register fragment",
		Summary: "Revised catalog record for a household register fragment",
		Source:  "Reading room box 18",
		Year:    1936,
		Tags:    []string{"register", "household", "marginalia"},
	})
	if err != nil {
		return err
	}
	if updated.Version != 2 || updated.Title != "Revised paper register fragment" {
		return fmt.Errorf("metadata update did not advance version: %#v", updated)
	}
	timeline, err := h.history(ctx, artifact.ID)
	if err != nil {
		return err
	}
	if len(timeline.Events) != 2 || timeline.Events[1].Action != domain.ActionUpdateMetadata {
		return fmt.Errorf("unexpected metadata timeline: %#v", timeline)
	}
	return nil
}

func checkSubmitReview(ctx context.Context, h *harness) error {
	artifact, err := h.create(ctx, "Railway timetable notes", "Working notes describing a regional railway timetable", []string{"railway", "timetable"})
	if err != nil {
		return err
	}
	submitted, err := h.submit(ctx, artifact.ID)
	if err != nil {
		return err
	}
	if submitted.Status != domain.StatusPendingReview || submitted.SubmittedAt == nil || submitted.Version != 2 {
		return fmt.Errorf("unexpected submitted artifact: %#v", submitted)
	}
	timeline, err := h.history(ctx, artifact.ID)
	if err != nil {
		return err
	}
	if len(timeline.Events) != 2 || timeline.Events[1].Action != domain.ActionSubmitReview {
		return fmt.Errorf("unexpected submission timeline: %#v", timeline)
	}
	return nil
}

func checkDecideReview(ctx context.Context, h *harness) error {
	artifact, err := h.create(ctx, "Botanical field card", "Field card describing plants observed near a wetland", []string{"botany", "wetland"})
	if err != nil {
		return err
	}
	submitted, err := h.submit(ctx, artifact.ID)
	if err != nil {
		return err
	}
	approved, err := h.review(ctx, submitted.ID, "approve", "curator-lin", "")
	if err != nil {
		return err
	}
	if approved.Status != domain.StatusApproved || approved.DecidedAt == nil || approved.Version != 3 {
		return fmt.Errorf("unexpected approved artifact: %#v", approved)
	}
	exported, err := h.exportArtifact(ctx, approved.ID)
	if err != nil {
		return err
	}
	if exported.ID != approved.ID || exported.Status != domain.StatusApproved {
		return fmt.Errorf("unexpected exported artifact: %#v", exported)
	}
	return nil
}

func checkSearchExport(ctx context.Context, h *harness) error {
	first, err := h.create(ctx, "Mining map annotation", "Annotation for a mining map from the north district", []string{"mining", "map"})
	if err != nil {
		return err
	}
	second, err := h.create(ctx, "Harbor shipping register", "Register entry describing vessel arrivals at the harbor", []string{"harbor", "register"})
	if err != nil {
		return err
	}
	if _, err := h.submit(ctx, first.ID); err != nil {
		return err
	}
	if _, err := h.review(ctx, first.ID, "approve", "curator-lin", ""); err != nil {
		return err
	}
	if _, err := h.submit(ctx, second.ID); err != nil {
		return err
	}
	collection, err := h.exportCollection(ctx, "tag=map")
	if err != nil {
		return err
	}
	if collection.Count != 1 || len(collection.Artifacts) != 1 || !catalog.VerifyCollectionChecksum(collection) {
		return fmt.Errorf("unexpected collection export: %#v", collection)
	}
	return nil
}

func checkImportBatch(ctx context.Context, h *harness) error {
	items := []domain.CreateArtifact{
		{
			Title:   "Canal field notebook",
			Summary: "Notebook recording canal measurements and field observations",
			Source:  "Engineering transfer 4",
			Year:    1918,
			Tags:    []string{"canal", "field-record"},
		},
		{
			Title:   "Market notice poster",
			Summary: "Printed notice describing market opening times and rules",
			Source:  "Broadside collection",
			Year:    1927,
			Tags:    []string{"market", "poster"},
		},
		{
			Title:   "School inspection report",
			Summary: "Inspection observations for a rural school and its facilities",
			Source:  "Education office box 6",
			Year:    1941,
			Tags:    []string{"school", "inspection"},
		},
	}
	result, err := h.importBatch(ctx, items)
	if err != nil {
		return err
	}
	if result.Count != len(items) {
		return fmt.Errorf("batch count = %d, want %d", result.Count, len(items))
	}
	summary, err := h.summary(ctx)
	if err != nil {
		return err
	}
	if summary.Total != len(items) || summary.Working != len(items) {
		return fmt.Errorf("unexpected summary after batch: %#v", summary)
	}
	return nil
}

func (h *harness) create(ctx context.Context, title, summary string, tags []string) (domain.Artifact, error) {
	input := domain.CreateArtifact{
		Title:   title,
		Summary: summary,
		Source:  "Reading room transfer",
		Year:    1932,
		Tags:    tags,
	}
	var artifact domain.Artifact
	if err := h.request(ctx, http.MethodPost, "/artifacts", input, &artifact); err != nil {
		return domain.Artifact{}, err
	}
	return artifact, nil
}

func (h *harness) getArtifact(ctx context.Context, id string) (domain.Artifact, error) {
	var artifact domain.Artifact
	if err := h.request(ctx, http.MethodGet, "/artifacts/"+id, nil, &artifact); err != nil {
		return domain.Artifact{}, err
	}
	return artifact, nil
}

func (h *harness) update(ctx context.Context, id string, input domain.CreateArtifact) (domain.Artifact, error) {
	var artifact domain.Artifact
	if err := h.request(ctx, http.MethodPut, "/artifacts/"+id+"/metadata", input, &artifact); err != nil {
		return domain.Artifact{}, err
	}
	return artifact, nil
}

func (h *harness) submit(ctx context.Context, id string) (domain.Artifact, error) {
	var artifact domain.Artifact
	if err := h.request(ctx, http.MethodPost, "/artifacts/"+id+"/submit", map[string]any{}, &artifact); err != nil {
		return domain.Artifact{}, err
	}
	return artifact, nil
}

func (h *harness) review(ctx context.Context, id, decision, reviewer, note string) (domain.Artifact, error) {
	body := map[string]string{"decision": decision, "reviewer": reviewer, "note": note}
	var artifact domain.Artifact
	if err := h.request(ctx, http.MethodPost, "/artifacts/"+id+"/review", body, &artifact); err != nil {
		return domain.Artifact{}, err
	}
	return artifact, nil
}

func (h *harness) history(ctx context.Context, id string) (domain.Timeline, error) {
	var timeline domain.Timeline
	if err := h.request(ctx, http.MethodGet, "/artifacts/"+id+"/history", nil, &timeline); err != nil {
		return domain.Timeline{}, err
	}
	return timeline, nil
}

func (h *harness) exportArtifact(ctx context.Context, id string) (domain.Artifact, error) {
	var artifact domain.Artifact
	if err := h.request(ctx, http.MethodGet, "/artifacts/"+id+"/export", nil, &artifact); err != nil {
		return domain.Artifact{}, err
	}
	return artifact, nil
}

func (h *harness) exportCollection(ctx context.Context, query string) (catalog.Collection, error) {
	path := "/collections/export?view=public"
	if strings.TrimSpace(query) != "" {
		path += "&" + strings.TrimSpace(query)
	}
	var collection catalog.Collection
	if err := h.request(ctx, http.MethodGet, path, nil, &collection); err != nil {
		return catalog.Collection{}, err
	}
	return collection, nil
}

func (h *harness) importBatch(ctx context.Context, items []domain.CreateArtifact) (catalog.BatchResult, error) {
	var result catalog.BatchResult
	if err := h.request(ctx, http.MethodPost, "/artifacts/batch", map[string]any{"actor": "batch-check", "items": items}, &result); err != nil {
		return catalog.BatchResult{}, err
	}
	return result, nil
}

func (h *harness) summary(ctx context.Context) (domain.CatalogSummary, error) {
	var summary domain.CatalogSummary
	if err := h.request(ctx, http.MethodGet, "/catalog/summary", nil, &summary); err != nil {
		return domain.CatalogSummary{}, err
	}
	return summary, nil
}

func (h *harness) request(ctx context.Context, method, path string, body any, target any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, h.base+path, payload)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("X-Archive-Actor", "workflow-check")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := h.client.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("read %s %s: %w", method, path, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s %s returned %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(data)))
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}
	return nil
}
