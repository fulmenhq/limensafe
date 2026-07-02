package catalog

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// RepoVisibility is a forge resolver's verdict for one codename→repo mapping
// (internal-brief Part B). The resolver MUST fail safe: a 404, auth failure, rate
// limit, or missing tooling resolves to RepoUnresolved — never RepoPublic — so
// an ambiguous answer can never demote a still-private codename (cf. internal-brief
// Part A pre-flight).
type RepoVisibility string

const (
	// RepoPublic is the ONLY verdict that produces an allowlist entry. It must
	// reflect an explicit, positive "this repo is public" signal from the forge.
	RepoPublic RepoVisibility = "public"
	// RepoNotPublic is an explicit non-public answer (private/internal). No
	// allowlist entry; the codename stays blocked.
	RepoNotPublic RepoVisibility = "not_public"
	// RepoUnresolved is the fail-safe verdict for any ambiguity (404, auth
	// failure, rate limit, missing gh, parse error). No allowlist entry.
	RepoUnresolved RepoVisibility = "unresolved"
)

// VisibilityResolver resolves a forge repository's current visibility. The repo
// argument is the right-hand side of a mapping line (e.g. "owner/repo").
//
// Implementations MUST fail safe: return RepoUnresolved for any error or
// ambiguity. Returning RepoPublic asserts a positive public signal and is the
// only verdict that relaxes the gate.
type VisibilityResolver interface {
	Resolve(ctx context.Context, repo string) RepoVisibility
}

// VisibilitySummary is the value-free accounting a caller reports to operators.
// It deliberately carries no codenames or repo names: a non-public repo name
// may itself be sensitive (discretion), so only counts cross the boundary.
type VisibilitySummary struct {
	Total      int
	Public     int // resolved public → allowlisted
	NotPublic  int // resolved private/internal → left blocked
	Unresolved int // fail-safe (404/auth/rate-limit/missing-gh) → left blocked
	Added      int // allowlist entries actually appended (public, minus dedup)
}

// visibilityMapSeparator divides a protected codename from its backing repo on
// each mapping line: CODENAME==>owner/repo. Reuses the term-list separator so
// the two authoring formats stay consistent.
const visibilityMapSeparator = termListSeparator

// visibilityAllowlistIDPrefix namespaces resolver-generated allowlist ids. The
// suffix is a hash of the codename, so ids are opaque (never carry protected
// vocabulary) and stable (re-running is idempotent).
const visibilityAllowlistIDPrefix = "al-vis-"

// BuildVisibilityAllowlist enriches a base catalog with allowlist entries for
// codenames whose backing repository currently resolves as public (internal-brief
// Part B). It reads CODENAME==>owner/repo mappings, asks the resolver for each
// repo's visibility, and appends a literal allowlist entry for every
// confirmed-public codename — failing safe on every other verdict.
//
// The base catalog is preserved faithfully (the existing document is edited in
// place at the YAML-node level, not re-synthesized), so no entity, rule, note,
// or salt is lost. The result is round-tripped through LoadBytes so the output
// is guaranteed schema- and loader-valid. Diagnostics and the returned summary
// are value-free.
func BuildVisibilityAllowlist(ctx context.Context, baseBytes []byte, mappings io.Reader, resolver VisibilityResolver) ([]byte, VisibilitySummary, error) {
	if resolver == nil {
		return nil, VisibilitySummary{}, fmt.Errorf("visibility: resolver is required")
	}
	base, err := LoadBytes(baseBytes)
	if err != nil {
		return nil, VisibilitySummary{}, fmt.Errorf("visibility: base catalog invalid: %w", err)
	}

	// Pre-index existing allowlist ids so re-runs are idempotent (a codename
	// already allowlisted is not appended twice).
	existing := map[string]struct{}{}
	for _, a := range base.Allowlist {
		existing[a.ID] = struct{}{}
	}

	pairs, err := parseVisibilityMap(mappings)
	if err != nil {
		return nil, VisibilitySummary{}, err
	}

	var summary VisibilitySummary
	var newEntries []allowlistEntryOut
	// parseVisibilityMap guarantees one entry per codename (exact dups collapsed,
	// conflicting dups rejected), so no per-codename dedup is needed here.
	for _, p := range pairs {
		summary.Total++

		switch resolver.Resolve(ctx, p.repo) {
		case RepoPublic:
			summary.Public++
			id := visibilityAllowlistID(p.term)
			if _, have := existing[id]; have {
				continue // already allowlisted; idempotent
			}
			existing[id] = struct{}{}
			newEntries = append(newEntries, allowlistEntryOut{
				ID:      id,
				Kind:    AllowlistKindLiteral,
				Pattern: p.term,
				// Precise posture: whole_word avoids over-suppressing (an
				// over-broad allowlist would hide real leaks); case_insensitive
				// matches how codenames appear in prose.
				Variants: &allowlistVariantsOut{CaseInsensitive: true, WholeWord: true},
				Reason:   "backing repository resolved public",
			})
			summary.Added++
		case RepoNotPublic:
			summary.NotPublic++
		default:
			summary.Unresolved++
		}
	}

	if len(newEntries) == 0 {
		// Nothing to add — return the base bytes unchanged so the operation is a
		// clean no-op rather than a gratuitous reformat.
		return baseBytes, summary, nil
	}

	sort.Slice(newEntries, func(i, j int) bool { return newEntries[i].ID < newEntries[j].ID })

	enriched, err := appendAllowlistEntries(baseBytes, newEntries)
	if err != nil {
		return nil, VisibilitySummary{}, err
	}
	if _, err := LoadBytes(enriched); err != nil {
		return nil, VisibilitySummary{}, fmt.Errorf("visibility: enriched catalog failed validation: %w", err)
	}
	return enriched, summary, nil
}

type visibilityPair struct {
	term string
	repo string
}

// parseVisibilityMap reads CODENAME==>owner/repo lines. Blank lines and lines
// beginning with '#' are ignored. Errors are line-numbered and value-free.
//
// Duplicate codenames are handled fail-closed (secrev): an EXACT duplicate (the
// same codename mapped to the same repo) is benign and collapses to one entry,
// but a CONFLICTING duplicate (the same codename mapped to a different repo) is
// an ambiguity — resolving it "first line wins" could allowlist off a public
// result while a later line points at a private/unresolved repo — so it is
// rejected as a value-free config error rather than silently deduped. The
// returned pairs therefore have one entry per codename, in first-seen order.
func parseVisibilityMap(r io.Reader) ([]visibilityPair, error) {
	var pairs []visibilityPair
	firstRepo := map[string]string{} // codename → repo (for conflict detection)
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, visibilityMapSeparator)
		if idx < 0 {
			return nil, &TermListError{Line: lineNo, Reason: fmt.Sprintf("missing %q separator", visibilityMapSeparator)}
		}
		term := strings.TrimSpace(line[:idx])
		repo := strings.TrimSpace(line[idx+len(visibilityMapSeparator):])
		if term == "" {
			return nil, &TermListError{Line: lineNo, Reason: "empty codename"}
		}
		if repo == "" {
			return nil, &TermListError{Line: lineNo, Reason: "empty repo reference"}
		}
		if prev, seen := firstRepo[term]; seen {
			if prev == repo {
				continue // exact duplicate line — benign, already recorded
			}
			// Value-free: neither the codename nor either repo is echoed. Only the
			// line number and the structural reason cross the boundary.
			return nil, &TermListError{Line: lineNo, Reason: "conflicting duplicate codename (same codename mapped to a different repo); resolve the ambiguity — a duplicate must not fail open"}
		}
		firstRepo[term] = repo
		pairs = append(pairs, visibilityPair{term: term, repo: repo})
	}
	if err := scanner.Err(); err != nil {
		// Value-free: a caller's io.Reader may surface a path or protected content.
		return nil, fmt.Errorf("visibility: read mappings failed")
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("visibility: no mappings found (expected CODENAME%sowner/repo lines)", visibilityMapSeparator)
	}
	return pairs, nil
}

// visibilityAllowlistID derives a stable, opaque, alias-free allowlist id from
// the codename — hash-based so it never carries protected vocabulary and a
// re-run produces the same id (idempotent merge).
func visibilityAllowlistID(term string) string {
	sum := sha256.Sum256([]byte(term))
	return visibilityAllowlistIDPrefix + hex.EncodeToString(sum[:])[:12]
}

// allowlistEntryOut / allowlistVariantsOut are minimal marshal structs so
// generated allowlist YAML carries only populated fields (mirrors the term-list
// builder's approach). variants is a pointer so it is omitted entirely when nil.
type allowlistEntryOut struct {
	ID       string                `yaml:"id"`
	Kind     string                `yaml:"kind"`
	Pattern  string                `yaml:"pattern"`
	Variants *allowlistVariantsOut `yaml:"variants,omitempty"`
	Reason   string                `yaml:"reason,omitempty"`
}

type allowlistVariantsOut struct {
	CaseInsensitive bool `yaml:"case_insensitive,omitempty"`
	WholeWord       bool `yaml:"whole_word"`
}

// appendAllowlistEntries adds entries to the base catalog's top-level
// `allowlist` sequence by editing the decoded YAML node tree, preserving every
// other field, note, and the existing allowlist (if any). The document is
// re-encoded from the edited tree.
func appendAllowlistEntries(baseBytes []byte, entries []allowlistEntryOut) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(baseBytes, &doc); err != nil {
		return nil, fmt.Errorf("visibility: decode base catalog failed")
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("visibility: base catalog is not a YAML mapping")
	}
	root := doc.Content[0]

	seq := findOrCreateAllowlistSeq(root)
	for _, e := range entries {
		node, err := allowlistEntryNode(e)
		if err != nil {
			return nil, err
		}
		seq.Content = append(seq.Content, node)
	}

	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("visibility: encode enriched catalog failed")
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("visibility: finalize enriched catalog failed")
	}
	return []byte(buf.String()), nil
}

// findOrCreateAllowlistSeq returns the sequence node for the root mapping's
// `allowlist` key, creating (and attaching) an empty sequence if absent.
func findOrCreateAllowlistSeq(root *yaml.Node) *yaml.Node {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "allowlist" {
			val := root.Content[i+1]
			if val.Kind != yaml.SequenceNode {
				val.Kind = yaml.SequenceNode
				val.Tag = "!!seq"
			}
			return val
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "allowlist"}
	seqNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content, keyNode, seqNode)
	return seqNode
}

func allowlistEntryNode(e allowlistEntryOut) (*yaml.Node, error) {
	b, err := yaml.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("visibility: marshal allowlist entry failed")
	}
	var n yaml.Node
	if err := yaml.Unmarshal(b, &n); err != nil {
		return nil, fmt.Errorf("visibility: build allowlist entry node failed")
	}
	if n.Kind != yaml.DocumentNode || len(n.Content) == 0 {
		return nil, fmt.Errorf("visibility: unexpected allowlist entry shape")
	}
	return n.Content[0], nil
}
