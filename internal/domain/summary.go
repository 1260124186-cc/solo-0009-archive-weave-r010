package domain

import "sort"

type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type CatalogSummary struct {
	Total        int            `json:"total"`
	Public       int            `json:"public"`
	Working      int            `json:"working"`
	ByStatus     map[Status]int `json:"by_status"`
	EarliestYear int            `json:"earliest_year,omitempty"`
	LatestYear   int            `json:"latest_year,omitempty"`
	TopTags      []TagCount     `json:"top_tags"`
}

func SummarizeArtifacts(values []Artifact) CatalogSummary {
	summary := CatalogSummary{ByStatus: map[Status]int{}, TopTags: []TagCount{}}
	counts := map[string]int{}
	for _, value := range values {
		summary.Total++
		summary.ByStatus[value.Status]++
		if value.IsPublic() {
			summary.Public++
		} else {
			summary.Working++
		}
		if summary.EarliestYear == 0 || value.Year < summary.EarliestYear {
			summary.EarliestYear = value.Year
		}
		if value.Year > summary.LatestYear {
			summary.LatestYear = value.Year
		}
		for _, tag := range value.Tags {
			counts[tag.Name]++
		}
	}
	for name, count := range counts {
		summary.TopTags = append(summary.TopTags, TagCount{Name: name, Count: count})
	}
	sort.Slice(summary.TopTags, func(i, j int) bool {
		if summary.TopTags[i].Count == summary.TopTags[j].Count {
			return summary.TopTags[i].Name < summary.TopTags[j].Name
		}
		return summary.TopTags[i].Count > summary.TopTags[j].Count
	})
	if len(summary.TopTags) > 12 {
		summary.TopTags = summary.TopTags[:12]
	}
	return summary
}
