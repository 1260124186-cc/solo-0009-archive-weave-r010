package domain

import "strings"

type Metadata struct {
	Title   string
	Summary string
	Source  string
	Year    int
	Tags    []Tag
}

func BuildMetadata(input CreateArtifact) Metadata {
	input = input.Normalized()
	return Metadata{
		Title:   input.Title,
		Summary: input.Summary,
		Source:  input.Source,
		Year:    input.Year,
		Tags:    NormalizeTags(input.Tags),
	}
}

func (m Metadata) SearchText() string {
	parts := []string{m.Title, m.Summary, m.Source}
	parts = append(parts, TagNames(m.Tags)...)
	return strings.ToLower(strings.Join(parts, " "))
}

func (m Metadata) SortedTagNames() []string {
	return TagNames(m.Tags)
}

func (m Metadata) IsHistorical() bool {
	return m.Year < 1950
}

func (m Metadata) HasRequiredFields() bool {
	return strings.TrimSpace(m.Title) != "" &&
		strings.TrimSpace(m.Summary) != "" &&
		strings.TrimSpace(m.Source) != "" &&
		len(m.Tags) > 0
}

func (m Metadata) WithAdditionalTag(tag string) Metadata {
	m.Tags = MergeTags(m.Tags, NormalizeTags([]string{tag}))
	return m
}

func (m Metadata) ApplyTo(artifact Artifact) Artifact {
	artifact.Title = strings.TrimSpace(m.Title)
	artifact.Summary = strings.TrimSpace(m.Summary)
	artifact.Source = strings.TrimSpace(m.Source)
	artifact.Year = m.Year
	artifact.Tags = CanonicalTags(m.Tags)
	return artifact
}
