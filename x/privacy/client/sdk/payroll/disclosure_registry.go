package payroll

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type DisclosureKeyScope string

const (
	DisclosureKeyScopeEmployee DisclosureKeyScope = "employee"
	DisclosureKeyScopeCompany  DisclosureKeyScope = "company"
	DisclosureKeyScopeAuditor  DisclosureKeyScope = "auditor"
	DisclosureKeyScopeExternal DisclosureKeyScope = "external"
)

type DisclosureKeyEntry struct {
	KeyID        string
	Scope        DisclosureKeyScope
	SubjectID    string
	PublicKeyHex string
	Version      string
	Active       bool
}

type DisclosureKeyRegistry interface {
	LookupDisclosureKey(ctx context.Context, scope DisclosureKeyScope, subjectID string) (*DisclosureKeyEntry, error)
}

type MemoryDisclosureKeyRegistry struct {
	mu      sync.Mutex
	entries map[string]DisclosureKeyEntry
}

func NewMemoryDisclosureKeyRegistry(entries []DisclosureKeyEntry) (*MemoryDisclosureKeyRegistry, error) {
	registry := &MemoryDisclosureKeyRegistry{entries: make(map[string]DisclosureKeyEntry, len(entries))}
	for _, entry := range entries {
		if err := registry.Add(entry); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *MemoryDisclosureKeyRegistry) Add(entry DisclosureKeyEntry) error {
	normalized, err := normalizeDisclosureKeyEntry(entry)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = make(map[string]DisclosureKeyEntry)
	}
	r.entries[disclosureKeyLookupKey(normalized.Scope, normalized.SubjectID)] = normalized
	return nil
}

func (r *MemoryDisclosureKeyRegistry) LookupDisclosureKey(ctx context.Context, scope DisclosureKeyScope, subjectID string) (*DisclosureKeyEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[disclosureKeyLookupKey(scope, subjectID)]
	if !ok || !entry.Active {
		return nil, fmt.Errorf("%w: disclosure key %s/%s", ErrInvalidPayrollInput, scope, subjectID)
	}
	cloned := entry
	return &cloned, nil
}

func normalizeDisclosureKeyEntry(entry DisclosureKeyEntry) (DisclosureKeyEntry, error) {
	entry.KeyID = strings.TrimSpace(entry.KeyID)
	entry.Scope = DisclosureKeyScope(strings.TrimSpace(string(entry.Scope)))
	entry.SubjectID = strings.TrimSpace(entry.SubjectID)
	entry.PublicKeyHex = strings.ToLower(strings.TrimSpace(entry.PublicKeyHex))
	entry.Version = strings.TrimSpace(entry.Version)
	if entry.KeyID == "" {
		return DisclosureKeyEntry{}, fmt.Errorf("%w: disclosure key_id is required", ErrInvalidPayrollInput)
	}
	switch entry.Scope {
	case DisclosureKeyScopeEmployee, DisclosureKeyScopeCompany, DisclosureKeyScopeAuditor, DisclosureKeyScopeExternal:
	default:
		return DisclosureKeyEntry{}, fmt.Errorf("%w: unsupported disclosure key scope %q", ErrInvalidPayrollInput, entry.Scope)
	}
	if entry.SubjectID == "" {
		return DisclosureKeyEntry{}, fmt.Errorf("%w: disclosure key subject_id is required", ErrInvalidPayrollInput)
	}
	if err := validateDisclosurePubKeyHex(entry.PublicKeyHex); err != nil {
		return DisclosureKeyEntry{}, err
	}
	return entry, nil
}

func disclosureKeyLookupKey(scope DisclosureKeyScope, subjectID string) string {
	return strings.TrimSpace(string(scope)) + "\x00" + strings.TrimSpace(subjectID)
}
