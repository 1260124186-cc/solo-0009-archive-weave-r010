package comparison

import (
	"sort"
	"strconv"

	"example.com/solo-0009-archive-weave/internal/domain"
)

// CompareArtifacts 对两个已冻结的档案快照做字段级比较。
// 结论只依赖入参快照内容，不读取任何可变存储，因此后续修订无法改写它。
func CompareArtifacts(reference, target domain.Artifact) domain.ComparisonItemResult {
	differences := make([]domain.FieldDifference, 0)

	addText := func(field, from, to string) {
		if from == to {
			return
		}
		differences = append(differences, domain.FieldDifference{
			Field: field,
			Text:  &domain.FieldDiff{Field: field, From: from, To: to},
		})
	}

	addText("title", reference.Title, target.Title)
	addText("summary", reference.Summary, target.Summary)
	addText("source", reference.Source, target.Source)
	addText("status", string(reference.Status), string(target.Status))
	addText("year", strconv.Itoa(reference.Year), strconv.Itoa(target.Year))

	referenceTags := tagNameSet(reference.Tags)
	targetTags := tagNameSet(target.Tags)
	if added, removed := setDelta(referenceTags, targetTags); len(added) > 0 || len(removed) > 0 {
		differences = append(differences, domain.FieldDifference{
			Field: "tags",
			Tags:  &domain.TagSetDiff{Added: added, Removed: removed},
		})
	}

	return domain.ComparisonItemResult{
		ReferenceID:      reference.ID,
		ReferenceVersion: reference.Version,
		TargetID:         target.ID,
		TargetVersion:    target.Version,
		Equal:            len(differences) == 0,
		Differences:      differences,
	}
}

func tagNameSet(tags []domain.Tag) map[string]struct{} {
	result := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		result[tag.Name] = struct{}{}
	}
	return result
}

// setDelta 返回相对引用的新增与移除标签，均按字典序输出。
func setDelta(reference, target map[string]struct{}) (added []string, removed []string) {
	added = make([]string, 0)
	removed = make([]string, 0)
	for name := range target {
		if _, ok := reference[name]; !ok {
			added = append(added, name)
		}
	}
	for name := range reference {
		if _, ok := target[name]; !ok {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}
