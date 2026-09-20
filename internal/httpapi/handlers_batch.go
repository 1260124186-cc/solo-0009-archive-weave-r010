package httpapi

import (
	"net/http"

	"example.com/solo-0009-archive-weave/internal/domain"
)

type batchRequest struct {
	Actor string                  `json:"actor,omitempty"`
	Items []domain.CreateArtifact `json:"items"`
}

func (s *Server) importArtifactBatch(w http.ResponseWriter, r *http.Request) {
	var input batchRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, domain.Invalid("body", "invalid JSON body"))
		return
	}
	actor := input.Actor
	if actor == "" {
		actor = requestActor(r)
	}
	result, err := s.service.ImportBatch(r.Context(), input.Items, actor)
	if err != nil {
		s.metrics.Failure()
		s.logger.Failure("import_artifact_batch", err)
		writeError(w, err)
		return
	}
	s.metrics.Write()
	writeJSON(w, http.StatusCreated, result)
}
