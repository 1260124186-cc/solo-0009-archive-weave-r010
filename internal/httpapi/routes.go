package httpapi

import (
	"net/http"

	"example.com/solo-0009-archive-weave/internal/domain"
)

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.health)
	s.mux.HandleFunc("GET /metrics", s.metricsHandler)
	s.mux.HandleFunc("GET /catalog/summary", s.catalogSummary)

	s.mux.HandleFunc("POST /artifacts", s.writable(s.createArtifact))
	s.mux.HandleFunc("POST /artifacts/batch", s.writable(s.importArtifactBatch))
	s.mux.HandleFunc("GET /artifacts", s.listArtifacts)
	s.mux.HandleFunc("GET /artifacts/{id}", s.getArtifact)
	s.mux.HandleFunc("PUT /artifacts/{id}/metadata", s.writable(s.updateArtifactMetadata))
	s.mux.HandleFunc("POST /artifacts/{id}/submit", s.writable(s.submitArtifact))
	s.mux.HandleFunc("POST /artifacts/{id}/review", s.writable(s.reviewArtifact))
	s.mux.HandleFunc("GET /artifacts/{id}/history", s.artifactHistory)
	s.mux.HandleFunc("GET /artifacts/{id}/export", s.exportArtifact)
	s.mux.HandleFunc("GET /collections/export", s.exportCollection)

	s.mux.HandleFunc("POST /comparisons", s.writable(s.withComparisons(s.submitComparison)))
	s.mux.HandleFunc("GET /comparisons", s.withComparisons(s.listComparisons))
	s.mux.HandleFunc("GET /comparisons/{id}", s.withComparisons(s.getComparison))
}

// withComparisons 在比较能力未接线时返回稳定错误，避免空指针。
func (s *Server) withComparisons(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.comparisons == nil {
			writeError(w, domain.Conflict("comparison service is not configured"))
			return
		}
		next(w, r)
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"service":   "archive-weave",
		"read_only": s.readOnly,
	})
}

func (s *Server) metricsHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.metrics.Snapshot())
}
