package domain

import (
	"strings"
	"unicode/utf8"
)

func ValidateText(field, value string, minimum, maximum int) error {
	trimmed := strings.TrimSpace(value)
	length := utf8.RuneCountInString(trimmed)
	if length < minimum {
		return Invalid(field, "value is too short")
	}
	if length > maximum {
		return Invalid(field, "value is too long")
	}
	return nil
}

func ValidateIdentifier(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return Invalid(field, "value is required")
	}
	if utf8.RuneCountInString(trimmed) > 128 {
		return Invalid(field, "value is too long")
	}
	return nil
}
