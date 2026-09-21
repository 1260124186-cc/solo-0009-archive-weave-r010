package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"example.com/solo-0009-archive-weave/internal/domain"
)

const maximumRequestBody = 1 << 20

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	field := ""
	message := "internal server error"
	var appError domain.AppError
	if errors.As(err, &appError) {
		code = string(appError.Code)
		field = appError.Field
		message = appError.Message
		switch appError.Code {
		case domain.ErrInvalidInput:
			status = http.StatusBadRequest
		case domain.ErrNotFound:
			status = http.StatusNotFound
		case domain.ErrConflict, domain.ErrState:
			status = http.StatusConflict
		case domain.ErrUnavailable:
			status = http.StatusServiceUnavailable
		}
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
			"field":   field,
		},
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maximumRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return domain.Invalid("body", "request must contain a single JSON value")
		}
		return err
	}
	return nil
}
