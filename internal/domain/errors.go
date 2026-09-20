package domain

import "fmt"

type ErrorCode string

const (
	ErrInvalidInput ErrorCode = "invalid_input"
	ErrNotFound     ErrorCode = "not_found"
	ErrConflict     ErrorCode = "conflict"
	ErrState        ErrorCode = "invalid_state"
)

type AppError struct {
	Code    ErrorCode
	Message string
	Field   string
}

func (e AppError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Field)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
func Invalid(field, message string) error {
	return AppError{Code: ErrInvalidInput, Field: field, Message: message}
}
func NotFound(message string) error { return AppError{Code: ErrNotFound, Message: message} }
func Conflict(message string) error { return AppError{Code: ErrConflict, Message: message} }
func State(message string) error    { return AppError{Code: ErrState, Message: message} }
