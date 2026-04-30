// Package output implements the redaction-safe output contract for
// limensafe. Every emitted string passes through the Redactor before
// serialization. See docs/decisions/ADR-0003-redaction-safe-output.md.
package output

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Alias is one literal pattern that the redactor should replace with an
// opaque marker referencing the entity it belongs to.
//
// All output-visible identifiers (catalog_id, entity_id, rule_id,
// replacement_id) must be alias-safe — they must not contain any catalog
// alias as a substring under the engine's normalization rules. The
// redactor does not enforce this; it is the catalog loader's
// responsibility (see docs/design/catalog-schema.md, "ID Safety Rule").
type Alias struct {
	Pattern         string
	EntityID        string
	CaseInsensitive bool
}

// Redactor wraps an alias set and provides string redaction.
//
// The replacement marker form is <r:entity_id>. Markers reference IDs
// that are guaranteed alias-free, so the marker itself cannot leak the
// protected vocabulary. Verbose mode may surface additional metadata;
// raw values are only revealed via the explicit --unsafe-reveal flag.
//
// Concurrency: NewRedactor is not goroutine-safe. Once constructed, a
// Redactor is read-only and safe for concurrent Redact calls.
//
// Implementation note (v0): uses Go's stdlib regexp with alternation
// over QuoteMeta-escaped patterns. Adequate for the v0 spike against
// synthetic-acme. A future v0.x switch to Aho-Corasick (already in
// go.mod for the engine) is straightforward — same alias set, same
// marker form, swap the matcher.
type Redactor struct {
	re            *regexp.Regexp
	aliasToEntity map[string]string
}

// NewRedactor builds a Redactor from the given aliases. The matcher is
// built once at construction; subsequent Redact calls are read-only.
//
// Empty patterns are silently skipped. Duplicate patterns (under the
// case-normalization key) keep the first registered (longer-pattern-
// first sort makes overlapping patterns resolve to the longer entity).
//
// Returns an error only if no usable aliases remain after filtering or
// if regex compilation fails.
func NewRedactor(aliases []Alias) (*Redactor, error) {
	sorted := append([]Alias(nil), aliases...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len(sorted[i].Pattern) > len(sorted[j].Pattern)
	})

	aliasToEntity := make(map[string]string, len(sorted))
	parts := make([]string, 0, len(sorted))
	for _, a := range sorted {
		if a.Pattern == "" {
			continue
		}
		key := a.Pattern
		if a.CaseInsensitive {
			key = strings.ToLower(key)
		}
		if _, ok := aliasToEntity[key]; ok {
			continue
		}
		aliasToEntity[key] = a.EntityID
		escaped := regexp.QuoteMeta(a.Pattern)
		if a.CaseInsensitive {
			escaped = "(?i:" + escaped + ")"
		}
		parts = append(parts, escaped)
	}

	if len(parts) == 0 {
		return nil, fmt.Errorf("output: NewRedactor requires at least one non-empty alias")
	}

	re, err := regexp.Compile(strings.Join(parts, "|"))
	if err != nil {
		return nil, fmt.Errorf("output: regex compile failed: %w", err)
	}

	return &Redactor{
		re:            re,
		aliasToEntity: aliasToEntity,
	}, nil
}

// Redact replaces any alias substring in s with the marker form
// <r:entity_id>. Returns s unchanged if no aliases match.
//
// Goroutine-safe.
func (r *Redactor) Redact(s string) string {
	if r == nil || r.re == nil {
		return s
	}
	return r.re.ReplaceAllStringFunc(s, r.markerFor)
}

func (r *Redactor) markerFor(match string) string {
	if id, ok := r.aliasToEntity[match]; ok {
		return marker(id)
	}
	if id, ok := r.aliasToEntity[strings.ToLower(match)]; ok {
		return marker(id)
	}
	// Should not happen given the matcher is built from aliasToEntity.
	// Fail closed: emit a generic marker so unknown matches still don't
	// leak the raw value.
	return "<r:?>"
}

func marker(entityID string) string {
	return "<r:" + entityID + ">"
}
