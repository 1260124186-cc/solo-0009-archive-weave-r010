package httpapi

import (
	"net/http"

	"example.com/solo-0009-archive-weave/internal/catalog"
	"example.com/solo-0009-archive-weave/internal/domain"
)

type comparisonRequest struct {
	Actor     string                `json:"actor,omitempty"`
	Reference comparisonTargetRef   `json:"reference"`
	Targets   []comparisonTargetRef `json:"targets"`
}

type comparisonTargetRef struct {
	ArtifactID string `json:"artifact_id"`
	Version    int    `json:"version,omitempty"`
}

func (s *Server) createComparison(w http.ResponseWriter, r *http.Request) {
	if s.comparison == nil {
		writeError(w, domain.Unavailable("comparison service is not configured"))
		return
	}
	var input comparisonRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, domain.Invalid("body", "invalid JSON body"))
		return
	}
	actor := input.Actor
	if actor == "" {
		actor = requestActor(r)
	}
	view, reused, err := s.comparison.SubmitComparison(r.Context(), catalog.ComparisonInput{
		Actor: actor,
		Reference: domain.VersionRef{
			ArtifactID: input.Reference.ArtifactID,
			Version:    input.Reference.Version,
		},
		Targets: convertTargetRefs(input.Targets),
	})
	if err != nil {
		s.metrics.Failure()
		s.logger.Failure("submit_comparison", err)
		writeError(w, err)
		return
	}
	s.metrics.Write()
	status := http.StatusCreated
	if reused {
		status = http.StatusOK
		w.Header().Set("X-Comparison-Reused", "true")
	}
	w.Header().Set("Location", "/comparisons/"+view.ID)
	writeJSON(w, status, view)
}

func (s *Server) getComparison(w http.ResponseWriter, r *http.Request) {
	if s.comparison == nil {
		writeError(w, domain.Unavailable("comparison service is not configured"))
		return
	}
	view, err := s.comparison.GetComparison(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) listComparisons(w http.ResponseWriter, r *http.Request) {
	if s.comparison == nil {
		writeError(w, domain.Unavailable("comparison service is not configured"))
		return
	}
	views, err := s.comparison.ListComparisons(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":       len(views),
		"comparisons": views,
	})
}

func (s *Server) resumeComparison(w http.ResponseWriter, r *http.Request) {
	if s.comparison == nil {
		writeError(w, domain.Unavailable("comparison service is not configured"))
		return
	}
	view, err := s.comparison.ResumeComparison(r.Context(), r.PathValue("id"))
	if err != nil {
		s.metrics.Failure()
		s.logger.Failure("resume_comparison", err)
		writeError(w, err)
		return
	}
	s.metrics.Write()
	writeJSON(w, http.StatusAccepted, view)
}

func convertTargetRefs(values []comparisonTargetRef) []domain.VersionRef {
	result := make([]domain.VersionRef, 0, len(values))
	for _, value := range values {
		result = append(result, domain.VersionRef{ArtifactID: value.ArtifactID, Version: value.Version})
	}
	return result
}
