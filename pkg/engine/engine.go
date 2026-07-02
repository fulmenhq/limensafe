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
	GitRef        string
	Line          int
	Column        int
	ReplacementID string
	EvidenceShape string
	Message       string
}

// Suppression records an entity match that the allowlist subtracted before it
// could become a finding (internal-brief "subtract the allowlist, then match"). It is
// redaction-safe: it carries the alias-safe allowlist entry id and entity id
// plus surface/location, never the matched text or the allowlist pattern.
//
// One suppression is recorded per (entity, unit) that had at least one
// allowlist-covered match but produced no finding anywhere in the unit — so the
// count reconciles cleanly with findings and "0 findings" can never silently
// mean "the allowlist ate everything."
type Suppression struct {
	AllowlistID string
	EntityID    string
	Surface     string
	SourceKind  string
	Path        string
	SourceID    string
	GitRef      string
}

// ScanResult is the full outcome of scanning one unit: the emitted findings and
// the allowlist suppressions that kept other entity matches from becoming
// findings.
type ScanResult struct {
	Findings     []Finding
	Suppressions []Suppression
}

// Scanner is safe for concurrent use after construction.
type Scanner struct {
	visibility     string
	blockThreshold string
	entities       []entityRule
	coRules        []coRule
	allowlist      []allowRule
}

// allowRule is one compiled catalog allowlist entry. Its match suppresses any
// entity finding whose span it fully covers. The id is alias-safe and
// output-visible; the patterns are catalog-private and never emitted.
type allowRule struct {
	id       string
	literals []literalRule
	regexes  []allowRegex
}

// allowRegex is a compiled allowlist regex plus its word-boundary flag. Case
// insensitivity is baked into the compiled pattern (a leading (?i)); whole_word
// is enforced at match time by boundary-checking the match span, mirroring how
// literal allowlist (and alias) whole-word matching works.
type allowRegex struct {
	re        *regexp.Regexp
	wholeWord bool
}

// allowSpan is a byte range in the scanned text that an allowlist entry matched,
// tagged with the entry id that produced it.
type allowSpan struct {
	start int
	end   int
	id    string
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
		for _, a := range c.Allowlist {
			rule, err := buildAllowRule(a)
			if err != nil {
				return nil, err
			}
			s.allowlist = append(s.allowlist, rule)
		}
	}
	return s, nil
}

func buildAllowRule(a catalog.AllowlistEntry) (allowRule, error) {
	r := allowRule{id: a.ID}
	switch a.Kind {
	case catalog.AllowlistKindRegex:
		// case_insensitive is applied with an RE2-safe leading (?i) flag so the
		// compiled matcher folds case itself; whole_word is enforced at match
		// time by boundary-checking the span (the regex source length is not the
		// match length, so the literal "<4 runes" default does not apply — for
		// regex, whole_word is opt-in via an explicit flag).
		pattern := a.Pattern
		if a.Variants.CaseInsensitive {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			// Value-free: the pattern is catalog-private (ADR-0003). The loader
			// already rejects bad regex with a value-free message; this is
			// defense-in-depth for direct engine callers.
			return allowRule{}, fmt.Errorf("engine: allowlist %s: regex does not compile", a.ID)
		}
		r.regexes = append(r.regexes, allowRegex{
			re:        re,
			wholeWord: a.Variants.WholeWordSet && a.Variants.WholeWord,
		})
	default: // literal (loader guarantees kind is literal or regex)
		r.literals = append(r.literals, newLiteralRule(a.Pattern, a.Variants.CaseInsensitive, wholeWordForAllowlist(a)))
	}
	return r, nil
}

// wholeWordForAllowlist mirrors wholeWordForAlias for literal allowlist entries:
// an explicit flag wins, otherwise short patterns (<4 runes) default to
// whole-word to avoid the substring class of over-broad allowlisting. (Regex
// entries do not use this — their whole_word is opt-in; see buildAllowRule.)
func wholeWordForAllowlist(a catalog.AllowlistEntry) bool {
	if a.Variants.WholeWordSet {
		return a.Variants.WholeWord
	}
	return utf8.RuneCountInString(strings.TrimSpace(a.Pattern)) < 4
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
			r.literals = append(r.literals, newLiteralRule(variant, e.Variants.CaseInsensitive, wholeWordForAlias(variant, e.Variants)))
		}
	}
	for _, token := range e.Tokens {
		r.literals = append(r.literals, newLiteralRule(token, false, true))
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

func newLiteralRule(pattern string, caseInsensitive, wholeWord bool) literalRule {
	lit := literalRule{
		pattern:   pattern,
		match:     pattern,
		ci:        caseInsensitive,
		wholeWord: wholeWord,
	}
	if lit.ci {
		lit.match = strings.ToLower(pattern)
	}
	return lit
}

// ScanUnit scans one input unit and returns findings. The unit content and
// location may contain protected values; findings never include raw evidence.
// It is a thin wrapper over ScanUnitResult for callers that do not need the
// allowlist-suppression accounting.
func (s *Scanner) ScanUnit(unit extractor.InputUnit) []Finding {
	return s.ScanUnitResult(unit).Findings
}

// ScanUnitResult scans one input unit and returns both the emitted findings and
// the allowlist suppressions (internal-brief). The allowlist is subtracted before
// entity matching is finalized: for each entity the engine takes the first
// occurrence NOT covered by an allowlist span; if every occurrence is covered
// the entity produces a suppression instead of a finding, and — because the
// suppressed entity is never recorded as "seen" — it also does not feed any
// co-occurrence rule. This is the corpus author's "subtract the allowlist, then
// match" contract.
func (s *Scanner) ScanUnitResult(unit extractor.InputUnit) ScanResult {
	if s == nil {
		return ScanResult{}
	}
	var findings []Finding
	var candidates []Suppression
	seenEntities := map[string]Finding{}

	pathFindings, pathCandidates := s.scanPath(unit)
	for _, f := range pathFindings {
		findings = append(findings, f)
		if _, ok := seenEntities[f.EntityID]; !ok && f.EntityID != "" {
			seenEntities[f.EntityID] = f
		}
	}
	candidates = append(candidates, pathCandidates...)

	content := string(unit.Content)
	surface := contentSurface(unit)
	allow := s.allowSpans(content)
	var contentLower string
	if hasCaseInsensitiveAllowlist(s.allowlist) || anyEntityCaseInsensitive(s.entities) {
		contentLower = strings.ToLower(content)
	}
	for _, r := range s.entities {
		if s.isAllowed(r) {
			continue
		}
		searchTextCI := content
		if hasCaseInsensitive(r) {
			searchTextCI = contentLower
		}
		matched := false
		coverID := ""
		for _, lit := range r.literals {
			searchText := content
			if lit.ci {
				searchText = searchTextCI
			}
			idx, found, cid := firstLiteralMatch(searchText, lit, allow)
			if found {
				line, col := lineColumn(content, idx)
				f := s.finding(unit, r, DetectorLiteral, surface, line, col, "literal", "")
				findings = append(findings, f)
				if _, ok := seenEntities[r.id]; !ok {
					seenEntities[r.id] = f
				}
				matched = true
				break
			}
			if cid != "" && coverID == "" {
				coverID = cid
			}
		}
		if !matched {
			for _, re := range r.regexes {
				idx, found, cid := firstRegexMatch(content, re, allow)
				if found {
					line, col := lineColumn(content, idx)
					f := s.finding(unit, r, DetectorRegex, surface, line, col, "regex", "")
					findings = append(findings, f)
					if _, ok := seenEntities[r.id]; !ok {
						seenEntities[r.id] = f
					}
					matched = true
					break
				}
				if cid != "" && coverID == "" {
					coverID = cid
				}
			}
		}
		if !matched && coverID != "" {
			candidates = append(candidates, s.suppression(unit, r, surface, coverID))
		}
	}

	findings = append(findings, s.coOccurrenceFindings(unit, seenEntities)...)
	assignFindingIDs(findings)
	return ScanResult{
		Findings:     findings,
		Suppressions: finalizeSuppressions(candidates, seenEntities),
	}
}

// finalizeSuppressions keeps, per entity, at most one suppression — and only for
// entities that produced no finding anywhere in the unit. An entity that matched
// on one surface (a real finding) and was allowlist-covered on another is a
// finding, not a suppression; this keeps suppression counts reconcilable with
// the one-finding-per-entity-per-unit model.
func finalizeSuppressions(candidates []Suppression, seen map[string]Finding) []Suppression {
	var out []Suppression
	emitted := map[string]bool{}
	for _, c := range candidates {
		if _, has := seen[c.EntityID]; has {
			continue
		}
		if emitted[c.EntityID] {
			continue
		}
		emitted[c.EntityID] = true
		out = append(out, c)
	}
	return out
}

func (s *Scanner) scanPath(unit extractor.InputUnit) ([]Finding, []Suppression) {
	if unit.Metadata["suppress_path"] == "true" {
		return nil, nil
	}
	if unit.SourceKind != "file" && unit.SourceKind != extractor.SourceKindGitHistoryBlob {
		return nil, nil
	}
	var findings []Finding
	var candidates []Suppression
	path := filepath.ToSlash(unit.SourceID)
	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	// Allowlist spans are scoped per segment, mirroring per-segment entity
	// matching: an allowlist literal/regex suppresses a path-segment finding it
	// covers within the same segment.
	segAllow := make([][]allowSpan, len(segments))
	for i, seg := range segments {
		segAllow[i] = s.allowSpans(seg)
	}
	for _, r := range s.entities {
		if s.isAllowed(r) {
			continue
		}
		matched := false
		coverID := ""
		for _, lit := range r.literals {
			for i, seg := range segments {
				got := seg
				if lit.ci {
					got = strings.ToLower(seg)
				}
				_, found, cid := firstLiteralMatch(got, lit, segAllow[i])
				if found {
					findings = append(findings, s.finding(unit, r, DetectorPathSegment, SurfacePath, 0, 0, "path_segment", ""))
					matched = true
					break
				}
				if cid != "" && coverID == "" {
					coverID = cid
				}
			}
			if matched {
				break
			}
		}
		if !matched && coverID != "" {
			candidates = append(candidates, s.suppression(unit, r, SurfacePath, coverID))
		}
	}
	return findings, candidates
}

// suppression builds a redaction-safe Suppression record for an entity whose
// matches were all allowlist-covered. It mirrors finding()'s surface/location
// handling but carries no severity/decision (a suppression is not a finding).
func (s *Scanner) suppression(unit extractor.InputUnit, r entityRule, surface, allowID string) Suppression {
	path := unit.SourceID
	switch unit.SourceKind {
	case SurfaceBranch, SurfaceCommit, extractor.SourceKindGitCommitMessage:
		path = ""
	}
	return Suppression{
		AllowlistID: allowID,
		EntityID:    r.id,
		Surface:     surface,
		SourceKind:  unit.SourceKind,
		Path:        path,
		SourceID:    unit.SourceID,
		GitRef:      unit.Metadata["git_ref"],
	}
}

func contentSurface(unit extractor.InputUnit) string {
	switch unit.SourceKind {
	case SurfaceBranch:
		return SurfaceBranch
	case SurfaceCommit:
		return SurfaceCommit
	case "git_diff":
		return SurfaceDiff
	case extractor.SourceKindGitCommitMessage:
		return SurfaceCommit
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
	path := unit.SourceID
	switch unit.SourceKind {
	case SurfaceBranch, SurfaceCommit, extractor.SourceKindGitCommitMessage:
		path = ""
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
		Path:          path,
		SourceID:      unit.SourceID,
		GitRef:        unit.Metadata["git_ref"],
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
	case extractor.SourceKindGitHistoryBlob:
		return "blob"
	case extractor.SourceKindGitCommitMessage:
		return "commit_message"
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

// RefreshFingerprint recomputes a finding fingerprint after location
// expansion. History scans use this after expanding one unique-blob finding
// into commit:path attributions.
func RefreshFingerprint(f Finding) Finding {
	f.Fingerprint = fingerprint(f)
	return f
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

func wholeWordForAlias(alias string, variants catalog.EntityVariants) bool {
	if variants.WholeWordSet {
		return variants.WholeWord
	}
	return utf8.RuneCountInString(alias) < 4
}

// firstLiteralMatch scans text for lit and returns the byte offset of the first
// occurrence NOT covered by an allowlist span (found=true). If every occurrence
// is allowlist-covered it returns found=false with coverID set to the entry id
// that covered the first such occurrence — the signal that this match was
// subtracted rather than simply absent. allow may be nil (no allowlist), in
// which case the first occurrence is always returned.
func firstLiteralMatch(text string, lit literalRule, allow []allowSpan) (idx int, found bool, coverID string) {
	if lit.match == "" {
		return -1, false, ""
	}
	offset := 0
	for offset <= len(text) {
		i := strings.Index(text[offset:], lit.match)
		if i < 0 {
			break
		}
		i += offset
		end := i + len(lit.match)
		if !lit.wholeWord || hasWordBoundaries(text, i, end) {
			if id := coveredBy(i, end, allow); id == "" {
				return i, true, ""
			} else if coverID == "" {
				coverID = id
			}
		}
		offset = i + 1
	}
	return -1, false, coverID
}

// firstRegexMatch is the regex analog of firstLiteralMatch: the first regex
// match not covered by an allowlist span, else the covering entry id.
func firstRegexMatch(content string, re *regexp.Regexp, allow []allowSpan) (idx int, found bool, coverID string) {
	for _, loc := range re.FindAllStringIndex(content, -1) {
		if id := coveredBy(loc[0], loc[1], allow); id == "" {
			return loc[0], true, ""
		} else if coverID == "" {
			coverID = id
		}
	}
	return -1, false, coverID
}

// coveredBy returns the id of the first allowlist span that FULLY covers the
// half-open byte range [start,end), or "" if none does. Full-span coverage is
// the contract: a partial overlap does not suppress (see docs/design).
func coveredBy(start, end int, allow []allowSpan) string {
	for _, a := range allow {
		if a.start <= start && end <= a.end {
			return a.id
		}
	}
	return ""
}

// allowSpans returns every byte range in text matched by the catalog allowlist,
// tagged with the producing entry id. Both literal and regex entries honor their
// case_insensitive and whole_word variant flags — literals via a lowercased
// working copy + span boundary check, regex via a compiled (?i) flag + the same
// span boundary check. Offsets are in text's byte space; case-insensitive
// matching is exact for ASCII and best-effort for non-ASCII case folding, as
// with the engine's line/column accounting.
func (s *Scanner) allowSpans(text string) []allowSpan {
	if len(s.allowlist) == 0 {
		return nil
	}
	var lower string
	lowerReady := false
	var out []allowSpan
	for _, a := range s.allowlist {
		for _, lit := range a.literals {
			search := text
			if lit.ci {
				if !lowerReady {
					lower = strings.ToLower(text)
					lowerReady = true
				}
				search = lower
			}
			offset := 0
			for offset <= len(search) {
				i := strings.Index(search[offset:], lit.match)
				if i < 0 {
					break
				}
				i += offset
				end := i + len(lit.match)
				if !lit.wholeWord || hasWordBoundaries(search, i, end) {
					out = append(out, allowSpan{start: i, end: end, id: a.id})
				}
				offset = i + 1
			}
		}
		for _, ar := range a.regexes {
			for _, loc := range ar.re.FindAllStringIndex(text, -1) {
				if ar.wholeWord && !hasWordBoundaries(text, loc[0], loc[1]) {
					continue
				}
				out = append(out, allowSpan{start: loc[0], end: loc[1], id: a.id})
			}
		}
	}
	return out
}

func hasCaseInsensitiveAllowlist(rules []allowRule) bool {
	for _, a := range rules {
		for _, lit := range a.literals {
			if lit.ci {
				return true
			}
		}
	}
	return false
}

func anyEntityCaseInsensitive(rules []entityRule) bool {
	for _, r := range rules {
		if hasCaseInsensitive(r) {
			return true
		}
	}
	return false
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
	_, _ = h.Write([]byte(f.GitRef))
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
