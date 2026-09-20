package httpapi

import (
	"net/http"

	"example.com/solo-0009-archive-weave/internal/catalog"
)

func (s *Server) exportArtifact(w http.ResponseWriter, r *http.Request) {
	data, err := s.service.ExportArtifact(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(data, '\n'))
}

func (s *Server) exportCollection(w http.ResponseWriter, r *http.Request) {
	query, err := catalog.ParseQuery(queryValues(r))
	if err != nil {
		writeError(w, err)
		return
	}
	collection, err := s.service.Export(r.Context(), query)
	if err != nil {
		writeError(w, err)
		return
	}
	data, err := catalog.EncodeCollection(collection)
	if err != nil {
		writeError(w, err)
		return
	}
	s.metrics.Write()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(data, '\n'))
}
