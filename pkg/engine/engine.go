// Package engine detects Confidential Context Leakage in extracted input units.
package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fulmenhq/limensafe/pkg/catalog"
	"github.com/fulmenhq/limensafe/pkg/extractor"
)

const (
	SurfaceContent = "content"
	SurfacePath    = "path"
	SurfaceBranch  = "branch_name"
	SurfaceCommit  = "commit_message"
	SurfaceDiff    = "diff"

	DecisionAllow = "allow"
	DecisionBlock = "block"
	DecisionWarn  = "warn"

	ConfidenceHigh = "high"

	DetectorLiteral      = "literal"
	DetectorPathSegment  = "path-segment"
	DetectorRegex        = "regex"
	DetectorCoOccurrence = "co-occurrence"
)

// Finding is the engine's redaction-safe finding model. It deliberately
// contains no raw matched text or snippets.
type Finding struct {
	ID            string
	Fingerprint   string
	Severity      string
	Confidence    string
	Decision      string
	EntityID      string
	EntityClass   string
	DetectorID    string
	RuleID        string
	SourceKind    string
	Surface       string
	SurfaceKind   string
	Path          string
	SourceID      string
	Line          int
	Column        int
	ReplacementID string
	EvidenceShape string
	Message       string
}

// Scanner is safe for concurrent use after construction.
type Scanner struct {
	visibility     string
	blockThreshold string
	entities       []entityRule
	coRules        []coRule
}

type ScannerOptions struct {
	BlockThreshold string
}

type entityRule struct {
	id          string
	class       string
	severity    string
	replacement string
	allowedIn   map[string]bool
	blockedIn   map[string]bool
	literals    []literalRule
	regexes     []*regexp.Regexp
}

type literalRule struct {
	pattern   string
	match     string
	ci        bool
	wholeWord bool
}

type coRule struct {
	id       string
	terms    []string
	severity string
	applyIn  map[string]bool
}

// NewScanner builds a deterministic v0 scanner from loaded catalogs.
func NewScanner(catalogs []*catalog.Catalog, visibility string) (*Scanner, error) {
	return NewScannerWithOptions(catalogs, visibility, ScannerOptions{})
}

func NewScannerWithOptions(catalogs []*catalog.Catalog, visibility string, opts ScannerOptions) (*Scanner, error) {
	if visibility == "" {
		visibility = "public_oss"
	}
	blockThreshold := opts.BlockThreshold
	if blockThreshold == "" {
		blockThreshold = "high"
	}
	if !validSeverity(blockThreshold) {
		return nil, fmt.Errorf("engine: block_threshold must be one of critical, high, medium, low, info")
	}
	s := &Scanner{visibility: visibility, blockThreshold: blockThreshold}
	for _, c := range catalogs {
		if c == nil {
			continue
		}
		for _, e := range c.Entities {
			rule, err := buildEntityRule(c, e)
			if err != nil {
				return nil, err
			}
			s.entities = append(s.entities, rule)
		}
		for _, r := range c.CoOccurrenceRules {
			s.coRules = append(s.coRules, coRule{
				id:       r.RuleID,
				terms:    append([]string(nil), r.Terms...),
				severity: firstNonEmpty(r.SeverityOverride, c.DefaultSeverity, "high"),
				applyIn:  stringSet(r.ApplyIn),
			})
		}
	}
	return s, nil
}

func buildEntityRule(c *catalog.Catalog, e catalog.Entity) (entityRule, error) {
	r := entityRule{
		id:          e.ID,
		class:       e.Class,
		severity:    firstNonEmpty(e.SeverityOverride, c.DefaultSeverity, "high"),
		replacement: e.ReplacementSuggestion,
		allowedIn:   stringSet(e.AllowedIn),
		blockedIn:   stringSet(e.BlockedIn),
	}
	for _, a := range e.Aliases {
		for _, variant := range expandAlias(a, e.Variants) {
			lit := literalRule{
				pattern:   variant,
				match:     variant,
				ci:        e.Variants.CaseInsensitive,
				wholeWord: wholeWordForAlias(variant, e.Variants),
			}
			if lit.ci {
				lit.match = strings.ToLower(variant)
			}
			r.literals = append(r.literals, lit)
		}
	}
	for _, p := range e.RegexPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return entityRule{}, fmt.Errorf("engine: entity %s regex %q: %w", e.ID, p, err)
		}
		r.regexes = append(r.regexes, re)
	}
	dedupeLiterals(&r)
	return r, nil
}

// ScanUnit scans one input unit and returns findings. The unit content and
// location may contain protected values; findings never include raw evidence.
func (s *Scanner) ScanUnit(unit extractor.InputUnit) []Finding {
	if s == nil {
		return nil
	}
	var findings []Finding
	seenEntities := map[string]Finding{}

	pathFindings := s.scanPath(unit)
	for _, f := range pathFindings {
		findings = append(findings, f)
		if _, ok := seenEntities[f.EntityID]; !ok && f.EntityID != "" {
			seenEntities[f.EntityID] = f
		}
	}

	content := string(unit.Content)
	surface := contentSurface(unit)
	for _, r := range s.entities {
		if s.isAllowed(r) {
			continue
		}
		text := content
		if hasCaseInsensitive(r) {
			text = strings.ToLower(content)
		}
		for _, lit := range r.literals {
			searchText := content
			if lit.ci {
				searchText = text
			}
			idx := literalIndex(searchText, lit)
			if idx < 0 {
				continue
			}
			line, col := lineColumn(content, idx)
			f := s.finding(unit, r, DetectorLiteral, surface, line, col, "literal", "")
			findings = append(findings, f)
			if _, ok := seenEntities[r.id]; !ok {
				seenEntities[r.id] = f
			}
			break
		}
		for _, re := range r.regexes {
			loc := re.FindStringIndex(content)
			if loc == nil {
				continue
			}
			line, col := lineColumn(content, loc[0])
			f := s.finding(unit, r, DetectorRegex, surface, line, col, "regex", "")
			findings = append(findings, f)
			if _, ok := seenEntities[r.id]; !ok {
				seenEntities[r.id] = f
			}
			break
		}
	}

	findings = append(findings, s.coOccurrenceFindings(unit, seenEntities)...)
	assignFindingIDs(findings)
	return findings
}

func (s *Scanner) scanPath(unit extractor.InputUnit) []Finding {
	if unit.SourceKind != "file" {
		return nil
	}
	var findings []Finding
	path := filepath.ToSlash(unit.SourceID)
	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	for _, r := range s.entities {
		if s.isAllowed(r) {
			continue
		}
		for _, lit := range r.literals {
			if pathSegmentMatch(segments, lit) {
				findings = append(findings, s.finding(unit, r, DetectorPathSegment, SurfacePath, 0, 0, "path_segment", ""))
				break
			}
		}
	}
	return findings
}

func contentSurface(unit extractor.InputUnit) string {
	switch unit.SourceKind {
	case SurfaceBranch:
		return SurfaceBranch
	case SurfaceCommit:
		return SurfaceCommit
	case "git_diff":
		return SurfaceDiff
	default:
		return SurfaceContent
	}
}

func (s *Scanner) coOccurrenceFindings(unit extractor.InputUnit, seen map[string]Finding) []Finding {
	var findings []Finding
	for _, r := range s.coRules {
		if len(r.applyIn) > 0 && !r.applyIn[s.visibility] {
			continue
		}
		var anchor Finding
		matched := true
		for i, term := range r.terms {
			f, ok := seen[term]
			if !ok {
				matched = false
				break
			}
			if i == 0 {
				anchor = f
			}
		}
		if !matched {
			continue
		}
		f := anchor
		f.Severity = r.severity
		f.DetectorID = DetectorCoOccurrence
		f.RuleID = r.id
		f.EvidenceShape = "co_occurrence"
		f.Message = "Confidential context co-occurrence rule fired"
		f.Fingerprint = fingerprint(f)
		findings = append(findings, f)
	}
	return findings
}

func (s *Scanner) finding(unit extractor.InputUnit, r entityRule, detector, surface string, line, col int, shape, ruleID string) Finding {
	if rawLine := unit.Metadata["line"]; rawLine != "" {
		if mappedLine, err := strconv.Atoi(rawLine); err == nil && mappedLine > 0 {
			line = mappedLine + line - 1
		}
	}
	f := Finding{
		Severity:      r.severity,
		Confidence:    ConfidenceHigh,
		Decision:      s.decisionForSeverity(r.severity),
		EntityID:      r.id,
		EntityClass:   r.class,
		DetectorID:    detector,
		RuleID:        ruleID,
		SourceKind:    unit.SourceKind,
		Surface:       surface,
		SurfaceKind:   surfaceKind(unit),
		Path:          unit.SourceID,
		SourceID:      unit.SourceID,
		Line:          line,
		Column:        col,
		ReplacementID: r.replacement,
		EvidenceShape: shape,
		Message:       "Confidential context detected",
	}
	f.Fingerprint = fingerprint(f)
	return f
}

func surfaceKind(unit extractor.InputUnit) string {
	switch unit.SourceKind {
	case "git_diff":
		return "diff"
	case SurfaceBranch:
		return "branch_name"
	case SurfaceCommit:
		return "commit_message"
	case "file":
		if unit.Metadata["stage"] == "index" {
			return "staged_index"
		}
		return "working_tree"
	default:
		return unit.SourceKind
	}
}

func (s *Scanner) isAllowed(r entityRule) bool {
	return r.allowedIn[s.visibility]
}

func expandAlias(alias string, variants catalog.EntityVariants) []string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return nil
	}
	seen := map[string]bool{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			seen[v] = true
		}
	}
	add(alias)
	if variants.Slug {
		slug := slugify(alias, '-')
		add(slug)
		add(strings.ReplaceAll(slug, "-", "_"))
		add(strings.ReplaceAll(slug, "-", ""))
	}
	if variants.Pluralize {
		add(alias + "s")
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func slugify(s string, sep rune) string {
	var b strings.Builder
	lastSep := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSep = false
			continue
		}
		if !lastSep {
			b.WriteRune(sep)
			lastSep = true
		}
	}
	return strings.Trim(string(b.String()), string(sep))
}

func pathSegmentMatch(segments []string, lit literalRule) bool {
	for _, seg := range segments {
		got := seg
		if lit.ci {
			got = strings.ToLower(seg)
		}
		if literalIndex(got, lit) >= 0 {
			return true
		}
	}
	return false
}

func wholeWordForAlias(alias string, variants catalog.EntityVariants) bool {
	if variants.WholeWordSet {
		return variants.WholeWord
	}
	return utf8.RuneCountInString(alias) < 4
}

func literalIndex(text string, lit literalRule) int {
	if lit.match == "" {
		return -1
	}
	offset := 0
	for offset <= len(text) {
		idx := strings.Index(text[offset:], lit.match)
		if idx < 0 {
			return -1
		}
		idx += offset
		if !lit.wholeWord || hasWordBoundaries(text, idx, idx+len(lit.match)) {
			return idx
		}
		offset = idx + 1
	}
	return -1
}

func hasWordBoundaries(s string, start, end int) bool {
	return (start == 0 || !isWordRune(runeBefore(s, start))) &&
		(end == len(s) || !isWordRune(runeAt(s, end)))
}

func runeBefore(s string, idx int) rune {
	r, _ := utf8.DecodeLastRuneInString(s[:idx])
	return r
}

func runeAt(s string, idx int) rune {
	r, _ := utf8.DecodeRuneInString(s[idx:])
	return r
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func lineColumn(s string, byteOffset int) (int, int) {
	line, col := 1, 1
	for i := 0; i < len(s) && i < byteOffset; {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		i += size
	}
	return line, col
}

func fingerprint(f Finding) string {
	h := sha256.New()
	_, _ = h.Write([]byte(f.EntityID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(f.RuleID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(f.DetectorID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(f.SourceKind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(f.Surface))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(f.SurfaceKind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(f.Path))
	_, _ = fmt.Fprintf(h, ":%d:%d", f.Line, f.Column)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func assignFindingIDs(findings []Finding) {
	for i := range findings {
		findings[i].ID = fmt.Sprintf("f-%04d", i+1)
	}
}

func (s *Scanner) decisionForSeverity(severity string) string {
	if severityRank(severity) >= severityRank(s.blockThreshold) {
		return DecisionBlock
	}
	return DecisionWarn
}

func validSeverity(severity string) bool {
	return severityRank(severity) > 0
}

func severityRank(severity string) int {
	switch severity {
	case "info":
		return 1
	case "low":
		return 2
	case "medium":
		return 3
	case "high":
		return 4
	case "critical":
		return 5
	default:
		return 0
	}
}

func hasCaseInsensitive(r entityRule) bool {
	for _, lit := range r.literals {
		if lit.ci {
			return true
		}
	}
	return false
}

func stringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range values {
		if v != "" {
			out[v] = true
		}
	}
	return out
}

func dedupeLiterals(r *entityRule) {
	seen := map[string]bool{}
	out := r.literals[:0]
	for _, lit := range r.literals {
		key := fmt.Sprintf("%s:%t", lit.match, lit.wholeWord)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, lit)
	}
	r.literals = out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
