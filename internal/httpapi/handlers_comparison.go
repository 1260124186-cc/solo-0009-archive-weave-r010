package httpapi

import (
	"net/http"
	"strings"

	"example.com/solo-0009-archive-weave/internal/comparison"
	"example.com/solo-0009-archive-weave/internal/domain"
)

type submitComparisonRequest struct {
	Reference domain.ComparisonTarget   `json:"reference"`
	Targets   []domain.ComparisonTarget `json:"targets"`
}

type submitComparisonResponse struct {
	Job       domain.ComparisonJob `json:"job"`
	Reused    bool                 `json:"reused"`
	StatusURL string               `json:"status_url"`
}

func (s *Server) submitComparison(w http.ResponseWriter, r *http.Request) {
	var body submitComparisonRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, domain.Invalid("body", "request body must be valid JSON"))
		return
	}
	if len(body.Targets) == 0 {
		writeError(w, domain.Invalid("targets", "at least one comparison target is required"))
		return
	}
	request := domain.ComparisonRequest{
		Actor:     requestActor(r),
		Reference: body.Reference,
		Targets:   body.Targets,
	}
	job, err := s.comparisons.Submit(r.Context(), request)
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusCreated
	if job.Reused {
		status = http.StatusOK
	}
	writeJSON(w, status, submitComparisonResponse{
		Job:       job,
		Reused:    job.Reused,
		StatusURL: "/comparisons/" + strings.TrimSpace(job.ID),
	})
}

func (s *Server) getComparison(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := s.comparisons.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) listComparisons(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.comparisons.List(r.Context(), comparison.ListLimitFromQuery(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count": len(jobs),
		"jobs":  jobs,
	})
}
