package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"example.com/solo-0009-archive-weave/internal/domain"
)

func readArtifacts(path string) ([]domain.Artifact, error) {
	return readJSONArray[domain.Artifact](path)
}

func writeArtifacts(path string, values []domain.Artifact) error {
	return writeJSONArray(path, values)
}

func readAuditEvents(path string) ([]domain.AuditEvent, error) {
	return readJSONArray[domain.AuditEvent](path)
}

func writeAuditEvents(path string, values []domain.AuditEvent) error {
	return writeJSONArray(path, values)
}

func readJSONArray[T any](path string) ([]T, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []T{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) == 0 {
		return []T{}, nil
	}
	var values []T
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if values == nil {
		values = []T{}
	}
	return values, nil
}

func writeJSONArray[T any](path string, values []T) error {
	if values == nil {
		values = []T{}
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data)
}

func writeFileAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if directory == "" {
		directory = "."
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", directory, err)
	}
	output, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	temporary := output.Name()
	cleanup := func() {
		_ = output.Close()
		_ = os.Remove(temporary)
	}
	if err := output.Chmod(0o644); err != nil {
		cleanup()
		return fmt.Errorf("set mode on %s: %w", temporary, err)
	}
	if _, err := output.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write %s: %w", temporary, err)
	}
	if err := output.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync %s: %w", temporary, err)
	}
	if err := output.Close(); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("close %s: %w", temporary, err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}

func cloneArtifact(value domain.Artifact) domain.Artifact {
	return value.Clone()
}

func cloneAuditEvent(value domain.AuditEvent) domain.AuditEvent {
	return value
}
