package catalog

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"example.com/solo-0009-archive-weave/internal/domain"
)

const (
	ViewPublic  = "public"
	ViewWorking = "working"

	SortRecent = "recent"
	SortOldest = "oldest"
	SortTitle  = "title"
)

type Query struct {
	Keyword string
	Tag     string
	Year    int
	Status  domain.Status
	View    string
	Sort    string
	Offset  int
	Limit   int
}

func DefaultQuery() Query {
	return Query{View: ViewPublic, Sort: SortRecent, Limit: 50}
}

func ParseQuery(values map[string]string) (Query, error) {
	query := DefaultQuery()
	query.Keyword = strings.TrimSpace(values["q"])
	query.Tag = domain.NormalizeTagName(values["tag"])
	query.View = strings.ToLower(strings.TrimSpace(values["view"]))
	query.Sort = strings.ToLower(strings.TrimSpace(values["sort"]))
	query.Status = domain.Status(strings.ToLower(strings.TrimSpace(values["status"])))
	if query.View == "" {
		query.View = ViewPublic
	}
	if query.View != ViewPublic && query.View != ViewWorking {
		return query, domain.Invalid("view", "must be public or working")
	}
	if query.Sort == "" {
		query.Sort = SortRecent
	}
	if query.Sort != SortRecent && query.Sort != SortOldest && query.Sort != SortTitle {
		return query, domain.Invalid("sort", "must be recent, oldest or title")
	}
	if query.Status != "" && !query.Status.Valid() {
		return query, domain.Invalid("status", "unknown status")
	}
	if query.View == ViewPublic && query.Status != "" && query.Status != domain.StatusApproved {
		return query, domain.Invalid("status", "public queries can only request approved artifacts")
	}
	if raw := strings.TrimSpace(values["year"]); raw != "" {
		year, err := strconv.Atoi(raw)
		if err != nil || year < 1000 || year > 2100 {
			return query, domain.Invalid("year", "year must be a valid four digit number")
		}
		query.Year = year
	}
	if raw := strings.TrimSpace(values["offset"]); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return query, domain.Invalid("offset", "offset must be a non-negative integer")
		}
		query.Offset = offset
	}
	if raw := strings.TrimSpace(values["limit"]); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			return query, domain.Invalid("limit", "limit must be between 1 and 200")
		}
		query.Limit = limit
	}
	if len([]rune(query.Keyword)) > 160 {
		return query, domain.Invalid("q", "keyword is too long")
	}
	return query, nil
}

func (q Query) includes(value domain.Artifact) bool {
	if q.Status != "" {
		return value.Status == q.Status
	}
	if q.View == ViewWorking {
		return true
	}
	return value.Status == domain.StatusApproved
}

func (q Query) Describe() string {
	parts := []string{"view=" + q.View}
	if q.Keyword != "" {
		parts = append(parts, "q="+q.Keyword)
	}
	if q.Tag != "" {
		parts = append(parts, "tag="+q.Tag)
	}
	if q.Year > 0 {
		parts = append(parts, fmt.Sprintf("year=%d", q.Year))
	}
	if q.Status != "" {
		parts = append(parts, "status="+string(q.Status))
	}
	parts = append(parts, "sort="+q.Sort)
	return strings.Join(parts, ",")
}

func (s *Service) List(ctx context.Context, query Query) ([]domain.Artifact, error) {
	values, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	filtered := filterArtifacts(values, query)
	sortArtifacts(filtered, query.Sort)
	start := query.Offset
	if start >= len(filtered) {
		return []domain.Artifact{}, nil
	}
	end := len(filtered)
	if query.Limit > 0 && start+query.Limit < end {
		end = start + query.Limit
	}
	return append([]domain.Artifact(nil), filtered[start:end]...), nil
}

func filterArtifacts(values []domain.Artifact, query Query) []domain.Artifact {
	result := make([]domain.Artifact, 0, len(values))
	keyword := strings.ToLower(strings.TrimSpace(query.Keyword))
	for _, value := range values {
		if !query.includes(value) {
			continue
		}
		metadata := domain.BuildMetadata(domain.CreateArtifact{
			Title:   value.Title,
			Summary: value.Summary,
			Source:  value.Source,
			Year:    value.Year,
			Tags:    domain.TagNames(value.Tags),
		})
		if keyword != "" && !strings.Contains(metadata.SearchText(), keyword) {
			continue
		}
		if query.Tag != "" && !domain.HasTag(value.Tags, query.Tag) {
			continue
		}
		if query.Year > 0 && value.Year != query.Year {
			continue
		}
		result = append(result, value)
	}
	return result
}

func sortArtifacts(values []domain.Artifact, order string) {
	sort.Slice(values, func(i, j int) bool {
		switch order {
		case SortOldest:
			if values[i].UpdatedAt.Equal(values[j].UpdatedAt) {
				return values[i].ID < values[j].ID
			}
			return values[i].UpdatedAt.Before(values[j].UpdatedAt)
		case SortTitle:
			left := strings.ToLower(values[i].Title)
			right := strings.ToLower(values[j].Title)
			if left == right {
				return values[i].ID < values[j].ID
			}
			return left < right
		default:
			if values[i].UpdatedAt.Equal(values[j].UpdatedAt) {
				return values[i].ID < values[j].ID
			}
			return values[i].UpdatedAt.After(values[j].UpdatedAt)
		}
	})
}
