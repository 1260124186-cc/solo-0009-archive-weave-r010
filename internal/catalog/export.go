package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"example.com/solo-0009-archive-weave/internal/domain"
)

type Collection struct {
	SchemaVersion int               `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
	Query         string            `json:"query"`
	Count         int               `json:"count"`
	Checksum      string            `json:"checksum"`
	Artifacts     []domain.Artifact `json:"artifacts"`
}

func (s *Service) Export(ctx context.Context, query Query) (Collection, error) {
	values, err := s.List(ctx, query)
	if err != nil {
		return Collection{}, err
	}
	for index := range values {
		values[index].Reviews = nil
		values[index].Tags = domain.CanonicalTags(values[index].Tags)
	}
	return Collection{
		SchemaVersion: 1,
		GeneratedAt:   s.clock().UTC(),
		Query:         query.Describe(),
		Count:         len(values),
		Artifacts:     values,
	}, nil
}

func (s *Service) ExportArtifact(ctx context.Context, id string) ([]byte, error) {
	value, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !value.IsPublic() {
		return nil, domain.State("only approved artifacts can be exported")
	}
	value.Reviews = nil
	value.Tags = domain.CanonicalTags(value.Tags)
	return json.MarshalIndent(value, "", "  ")
}

func EncodeCollection(value Collection) ([]byte, error) {
	value.Checksum = ""
	unsigned, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(unsigned)
	value.Checksum = hex.EncodeToString(sum[:])
	return json.MarshalIndent(value, "", "  ")
}

func VerifyCollectionChecksum(value Collection) bool {
	provided := value.Checksum
	value.Checksum = ""
	unsigned, err := json.Marshal(value)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(unsigned)
	return provided == hex.EncodeToString(sum[:])
}
