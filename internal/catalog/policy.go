package catalog

import (
	"strings"
	"unicode/utf8"

	"example.com/solo-0009-archive-weave/internal/domain"
)

type Policy struct {
	MinimumSummary int
	MaxBatchSize   int
}

func DefaultPolicy() Policy {
	return Policy{MinimumSummary: 12, MaxBatchSize: 50}
}

func (p Policy) CheckCreate(input domain.CreateArtifact) error {
	summaryLength := utf8.RuneCountInString(strings.TrimSpace(input.Summary))
	if summaryLength < p.MinimumSummary {
		return domain.Invalid("summary", "summary is too short")
	}
	return nil
}

func (p Policy) CheckBatchSize(size int) error {
	if size < 1 {
		return domain.Invalid("items", "at least one item is required")
	}
	if size > p.MaxBatchSize {
		return domain.Invalid("items", "batch contains too many items")
	}
	return nil
}
