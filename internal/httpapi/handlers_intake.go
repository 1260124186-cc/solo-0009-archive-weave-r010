package httpapi

import (
	"net/http"

	"example.com/solo-0009-archive-weave/internal/domain"
)

func (s *Server) createArtifact(w http.ResponseWriter, r *http.Request) {
	var input domain.CreateArtifact
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, domain.Invalid("body", "invalid JSON body"))
		return
	}
	value, err := s.service.CreateAs(r.Context(), input, requestActor(r))
	if err != nil {
		s.metrics.Failure()
		s.logger.Failure("create_artifact", err)
		writeError(w, err)
		return
	}
	s.metrics.Write()
	writeJSON(w, http.StatusCreated, value)
}

func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) updateArtifactMetadata(w http.ResponseWriter, r *http.Request) {
	var input domain.CreateArtifact
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, domain.Invalid("body", "invalid JSON body"))
		return
	}
	value, err := s.service.UpdateMetadata(r.Context(), r.PathValue("id"), input, requestActor(r))
	if err != nil {
		s.metrics.Failure()
		s.logger.Failure("update_artifact_metadata", err)
		writeError(w, err)
		return
	}
	s.metrics.Write()
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) submitArtifact(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.Submit(r.Context(), r.PathValue("id"), requestActor(r))
	if err != nil {
		s.metrics.Failure()
		s.logger.Failure("submit_artifact", err)
		writeError(w, err)
		return
	}
	s.metrics.Write()
	writeJSON(w, http.StatusOK, value)
}
