package comparison

import (
	"context"
	"sync"
	"time"

	"example.com/solo-0009-archive-weave/internal/domain"
)

type readWriteMutex struct{ sync.RWMutex }

func (m *readWriteMutex) lock()    { m.Lock() }
func (m *readWriteMutex) unlock()  { m.Unlock() }
func (m *readWriteMutex) rLock()   { m.RLock() }
func (m *readWriteMutex) rUnlock() { m.RUnlock() }

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func cloneJob(job domain.ComparisonJob) domain.ComparisonJob {
	cloned := job
	cloned.Items = append([]domain.ComparisonItem(nil), job.Items...)
	for index := range cloned.Items {
		cloned.Items[index] = cloneItem(cloned.Items[index])
	}
	if job.StartedAt != nil {
		value := *job.StartedAt
		cloned.StartedAt = &value
	}
	if job.FinishedAt != nil {
		value := *job.FinishedAt
		cloned.FinishedAt = &value
	}
	return cloned
}

func cloneItem(item domain.ComparisonItem) domain.ComparisonItem {
	cloned := item
	if item.StartedAt != nil {
		value := *item.StartedAt
		cloned.StartedAt = &value
	}
	if item.FinishedAt != nil {
		value := *item.FinishedAt
		cloned.FinishedAt = &value
	}
	if item.Result != nil {
		result := *item.Result
		// 保留非空切片形态，使"无差异"在 JSON 中输出 [] 而非 null。
		result.Differences = make([]domain.FieldDifference, len(item.Result.Differences))
		copy(result.Differences, item.Result.Differences)
		for index := range result.Differences {
			diff := result.Differences[index]
			if diff.Text != nil {
				text := *diff.Text
				diff.Text = &text
			}
			if diff.Tags != nil {
				tags := *diff.Tags
				tags.Added = make([]string, len(diff.Tags.Added))
				copy(tags.Added, diff.Tags.Added)
				tags.Removed = make([]string, len(diff.Tags.Removed))
				copy(tags.Removed, diff.Tags.Removed)
				diff.Tags = &tags
			}
			result.Differences[index] = diff
		}
		cloned.Result = &result
	}
	return cloned
}

func timePointer(value time.Time) *time.Time {
	return &value
}
