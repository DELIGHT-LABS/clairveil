package payroll

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FileArtifactKind string

const (
	FileArtifactKindPlan              FileArtifactKind = "plans"
	FileArtifactKindPlanReport        FileArtifactKind = "plan-reports"
	FileArtifactKindNotePreparation   FileArtifactKind = "note-preparation-reports"
	FileArtifactKindDisclosureKeyList FileArtifactKind = "disclosure-keys"
)

type FileArtifactStore struct {
	Dir string
}

func (s FileArtifactStore) WritePayrollPlan(ctx context.Context, artifactID string, plan PayrollPlan) (string, error) {
	return s.writeJSON(ctx, FileArtifactKindPlan, artifactID, plan)
}

func (s FileArtifactStore) ReadPayrollPlan(ctx context.Context, artifactID string) (*PayrollPlan, error) {
	path, err := s.pathFor(FileArtifactKindPlan, artifactID)
	return readFileArtifact[PayrollPlan](ctx, path, err)
}

func (s FileArtifactStore) WritePlanReport(ctx context.Context, artifactID string, report PlanReport) (string, error) {
	return s.writeJSON(ctx, FileArtifactKindPlanReport, artifactID, report)
}

func (s FileArtifactStore) ReadPlanReport(ctx context.Context, artifactID string) (*PlanReport, error) {
	path, err := s.pathFor(FileArtifactKindPlanReport, artifactID)
	return readFileArtifact[PlanReport](ctx, path, err)
}

func (s FileArtifactStore) WriteNotePreparationReport(ctx context.Context, artifactID string, report NotePreparationReport) (string, error) {
	return s.writeJSON(ctx, FileArtifactKindNotePreparation, artifactID, report)
}

func (s FileArtifactStore) ReadNotePreparationReport(ctx context.Context, artifactID string) (*NotePreparationReport, error) {
	path, err := s.pathFor(FileArtifactKindNotePreparation, artifactID)
	return readFileArtifact[NotePreparationReport](ctx, path, err)
}

func (s FileArtifactStore) WriteDisclosureKeyEntries(ctx context.Context, artifactID string, entries []DisclosureKeyEntry) (string, error) {
	normalized := make([]DisclosureKeyEntry, len(entries))
	for i, entry := range entries {
		converted, err := normalizeDisclosureKeyEntry(entry)
		if err != nil {
			return "", err
		}
		normalized[i] = converted
	}
	return s.writeJSON(ctx, FileArtifactKindDisclosureKeyList, artifactID, normalized)
}

func (s FileArtifactStore) ReadDisclosureKeyEntries(ctx context.Context, artifactID string) ([]DisclosureKeyEntry, error) {
	path, pathErr := s.pathFor(FileArtifactKindDisclosureKeyList, artifactID)
	entries, err := readFileArtifact[[]DisclosureKeyEntry](ctx, path, pathErr)
	if err != nil {
		return nil, err
	}
	for i, entry := range *entries {
		normalized, err := normalizeDisclosureKeyEntry(entry)
		if err != nil {
			return nil, err
		}
		(*entries)[i] = normalized
	}
	return *entries, nil
}

func (s FileArtifactStore) ReadDisclosureKeyRegistry(ctx context.Context, artifactID string) (*MemoryDisclosureKeyRegistry, error) {
	entries, err := s.ReadDisclosureKeyEntries(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	return NewMemoryDisclosureKeyRegistry(entries)
}

func (s FileArtifactStore) writeJSON(ctx context.Context, kind FileArtifactKind, artifactID string, value any) (string, error) {
	path, err := s.pathFor(kind, artifactID)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", err
	}
	return path, nil
}

func (s FileArtifactStore) pathFor(kind FileArtifactKind, artifactID string) (string, error) {
	baseDir := strings.TrimSpace(s.Dir)
	if baseDir == "" {
		return "", fmt.Errorf("%w: file artifact store dir is required", ErrInvalidPayrollInput)
	}
	switch kind {
	case FileArtifactKindPlan, FileArtifactKindPlanReport, FileArtifactKindNotePreparation, FileArtifactKindDisclosureKeyList:
	default:
		return "", fmt.Errorf("%w: unsupported file artifact kind %q", ErrInvalidPayrollInput, kind)
	}
	safeID, err := safeFileArtifactID(artifactID)
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, string(kind), safeID+".json"), nil
}

func readFileArtifact[T any](ctx context.Context, path string, pathErr error) (*T, error) {
	if pathErr != nil {
		return nil, pathErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value T
	decoder := json.NewDecoder(bytes.NewReader(bz))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return &value, nil
}

func safeFileArtifactID(artifactID string) (string, error) {
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return "", fmt.Errorf("%w: file artifact id is required", ErrInvalidPayrollInput)
	}
	if artifactID == "." || artifactID == ".." {
		return "", fmt.Errorf("%w: file artifact id cannot be a path component", ErrInvalidPayrollInput)
	}
	for _, r := range artifactID {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return "", fmt.Errorf("%w: file artifact id %q contains unsupported character %q", ErrInvalidPayrollInput, artifactID, r)
		}
	}
	return artifactID, nil
}
