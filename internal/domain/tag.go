package domain

import (
	"sort"
	"strings"
)

type Tag struct {
	Name  string `json:"name"`
	Alias string `json:"alias,omitempty"`
}

func NormalizeTags(values []string) []Tag {
	result := make([]Tag, 0, len(values))
	seen := map[string]bool{}
	for _, raw := range values {
		name := NormalizeTagName(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, Tag{Name: name})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func NormalizeTagName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func TagNames(tags []Tag) []string {
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		if name := NormalizeTagName(tag.Name); name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func HasTag(tags []Tag, wanted string) bool {
	needle := NormalizeTagName(wanted)
	if needle == "" {
		return false
	}
	for _, tag := range tags {
		if NormalizeTagName(tag.Name) == needle || NormalizeTagName(tag.Alias) == needle {
			return true
		}
	}
	return false
}

func MergeTags(existing, incoming []Tag) []Tag {
	result := append([]Tag(nil), existing...)
	for _, tag := range incoming {
		if !HasTag(result, tag.Name) {
			result = append(result, Tag{Name: NormalizeTagName(tag.Name), Alias: strings.TrimSpace(tag.Alias)})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func CanonicalTags(tags []Tag) []Tag {
	names := TagNames(tags)
	result := make([]Tag, 0, len(names))
	aliases := map[string]string{}
	for _, tag := range tags {
		name := NormalizeTagName(tag.Name)
		if name == "" {
			continue
		}
		if alias := NormalizeTagName(tag.Alias); alias != "" && alias != name {
			aliases[name] = alias
		}
	}
	for _, name := range names {
		result = append(result, Tag{Name: name, Alias: aliases[name]})
	}
	return result
}
