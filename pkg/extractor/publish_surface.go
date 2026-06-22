package extractor

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Publish-surface ref inventory (internal-brief). Enumerates the refs a repository
// would expose when made public — every branch and tag on the publish remote
// (or local refs) — so the audit-publish command can scan each and flag
// leak-vector refs. This file owns ref enumeration, primary-ref resolution,
// and danger-name matching; blob attribution + dedup live alongside in the
// publish-surface scan path.

const (
	// RefKindBranch / RefKindTag classify an enumerated ref.
	RefKindBranch = "branch"
	RefKindTag    = "tag"
)

// DefaultDangerPatterns are the ref-name globs that a history rewrite's
// safety-net residue typically matches. `*` matches any run of characters
// including `/`. These are flagged leak_vector by name even before (or
// without) scanning content. Operator-overridable.
var DefaultDangerPatterns = []string{
	"backup/*",
	"*pre-rewrite*",
	"*-snapshot-*",
	"archive/*",
	"*-bak",
	"wip/*",
}

// RemoteRef is one ref on the publish surface.
type RemoteRef struct {
	// Name is the short ref name: "main", "backup/old", "v0.1.0".
	Name string
	// FullName is the fully-qualified ref: "refs/heads/main",
	// "refs/tags/v0.1.0".
	FullName string
	// Kind is RefKindBranch or RefKindTag.
	Kind string
	// Tip is the tip commit SHA the ref resolves to. For an annotated tag this
	// is the peeled commit (the `^{}` line from ls-remote) when available.
	Tip string
	// LocalObjects is true when Tip is present in the local object store and
	// the ref's content can therefore be scanned without a fetch.
	LocalObjects bool
}

// RefEnumerationOptions configures which refs make up the publish surface.
type RefEnumerationOptions struct {
	// Remote is the publish remote to inventory (default "origin"). Ignored
	// when IncludeLocal is set and Remote is empty.
	Remote string
	// IncludeLocal enumerates local refs (refs/heads + refs/tags) instead of
	// the remote. The default (false) inventories the remote — what actually
	// goes public.
	IncludeLocal bool
	// Branches / Tags select which ref kinds to include. Both default true at
	// the call site; tags are part of the publish surface and a common blind
	// spot (a tag can pin a pre-rewrite tree).
	Branches bool
	Tags     bool
}

// EnumerateRefs returns the publish-surface refs for the repo, sorted by
// full name. Remote enumeration uses `git ls-remote`; local uses
// `git for-each-ref`. Each ref is checked for local object availability so the
// caller can distinguish "scannable now" from "exists on the remote but not
// fetched".
func EnumerateRefs(ctx context.Context, repoRoot string, opts RefEnumerationOptions) ([]RemoteRef, error) {
	if !opts.Branches && !opts.Tags {
		return nil, fmt.Errorf("publish-surface: at least one of branches or tags must be selected")
	}
	var (
		refs []RemoteRef
		err  error
	)
	if opts.IncludeLocal {
		refs, err = enumerateLocalRefs(ctx, repoRoot, opts)
	} else {
		refs, err = enumerateRemoteRefs(ctx, repoRoot, opts)
	}
	if err != nil {
		return nil, err
	}
	for i := range refs {
		refs[i].LocalObjects = objectExistsLocally(ctx, repoRoot, refs[i].Tip)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].FullName < refs[j].FullName })
	return refs, nil
}

func enumerateRemoteRefs(ctx context.Context, repoRoot string, opts RefEnumerationOptions) ([]RemoteRef, error) {
	remote := opts.Remote
	if remote == "" {
		remote = "origin"
	}
	args := []string{"ls-remote"}
	if opts.Branches {
		args = append(args, "--heads")
	}
	if opts.Tags {
		args = append(args, "--tags")
	}
	args = append(args, remote)
	out, err := gitOutputBytes(ctx, repoRoot, args...)
	if err != nil {
		return nil, fmt.Errorf("publish-surface: git ls-remote %s: %w", remote, err)
	}
	// ls-remote lines: "<sha>\t<refname>". Annotated tags also emit a peeled
	// "<sha>\t<refname>^{}" line whose SHA is the commit the tag points at;
	// prefer that commit as the tip so tag content scans resolve correctly.
	byFull := map[string]*RemoteRef{}
	var order []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		sha, full, ok := splitLSRemoteLine(line)
		if !ok {
			continue
		}
		peeled := strings.HasSuffix(full, "^{}")
		base := strings.TrimSuffix(full, "^{}")
		kind, name, ok := classifyRef(base)
		if !ok {
			continue
		}
		ref, seen := byFull[base]
		if !seen {
			ref = &RemoteRef{Name: name, FullName: base, Kind: kind, Tip: sha}
			byFull[base] = ref
			order = append(order, base)
		}
		if peeled {
			// Peeled commit wins as the scannable tip for annotated tags.
			ref.Tip = sha
		}
	}
	refs := make([]RemoteRef, 0, len(order))
	for _, full := range order {
		refs = append(refs, *byFull[full])
	}
	return refs, nil
}

func enumerateLocalRefs(ctx context.Context, repoRoot string, opts RefEnumerationOptions) ([]RemoteRef, error) {
	var patterns []string
	if opts.Branches {
		patterns = append(patterns, "refs/heads")
	}
	if opts.Tags {
		patterns = append(patterns, "refs/tags")
	}
	// %(objectname) is the ref's own object; %(*objectname) is the peeled
	// commit for annotated tags (empty otherwise).
	args := append([]string{"for-each-ref", "--format=%(objectname) %(*objectname) %(refname)"}, patterns...)
	out, err := gitOutputBytes(ctx, repoRoot, args...)
	if err != nil {
		return nil, fmt.Errorf("publish-surface: git for-each-ref: %w", err)
	}
	var refs []RemoteRef
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		obj, peeled, full := fields[0], "", fields[len(fields)-1]
		if len(fields) == 3 {
			peeled = fields[1]
		}
		kind, name, ok := classifyRef(full)
		if !ok {
			continue
		}
		tip := obj
		if peeled != "" {
			tip = peeled
		}
		refs = append(refs, RemoteRef{Name: name, FullName: full, Kind: kind, Tip: tip})
	}
	return refs, nil
}

// ResolvePrimaryRef determines the primary ref the divergence detector
// compares against. An explicit override (short or full name) wins. Otherwise
// the remote's HEAD symref is used (`git ls-remote --symref <remote> HEAD`),
// falling back to "main". The returned name is the short ref name.
func ResolvePrimaryRef(ctx context.Context, repoRoot, remote, override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		_, name, ok := classifyRef(override)
		if ok {
			return name, nil
		}
		return strings.TrimSpace(override), nil
	}
	if remote == "" {
		remote = "origin"
	}
	out, err := gitOutputBytes(ctx, repoRoot, "ls-remote", "--symref", remote, "HEAD")
	if err != nil {
		// Remote may be unreachable; fall back rather than failing the audit.
		return "main", nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ref:") {
			continue
		}
		// "ref: refs/heads/main\tHEAD"
		rest := strings.TrimSpace(strings.TrimPrefix(line, "ref:"))
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		if _, name, ok := classifyRef(fields[0]); ok {
			return name, nil
		}
	}
	return "main", nil
}

// MatchesDangerPattern reports whether a short ref name matches any of the
// danger-name globs (where `*` matches any run including `/`), returning the
// first matching pattern.
func MatchesDangerPattern(name string, patterns []string) (string, bool) {
	for _, pat := range patterns {
		re, err := globToRegexp(pat)
		if err != nil {
			continue
		}
		if re.MatchString(name) {
			return pat, true
		}
	}
	return "", false
}

func classifyRef(full string) (kind, name string, ok bool) {
	switch {
	case strings.HasPrefix(full, "refs/heads/"):
		return RefKindBranch, strings.TrimPrefix(full, "refs/heads/"), true
	case strings.HasPrefix(full, "refs/tags/"):
		return RefKindTag, strings.TrimPrefix(full, "refs/tags/"), true
	default:
		return "", "", false
	}
}

func splitLSRemoteLine(line string) (sha, full string, ok bool) {
	tab := strings.IndexByte(line, '\t')
	if tab < 0 {
		return "", "", false
	}
	sha = strings.TrimSpace(line[:tab])
	full = strings.TrimSpace(line[tab+1:])
	if sha == "" || full == "" {
		return "", "", false
	}
	return sha, full, true
}

func objectExistsLocally(ctx context.Context, repoRoot, sha string) bool {
	if strings.TrimSpace(sha) == "" {
		return false
	}
	// `cat-file -e <sha>` exits 0 iff the object is present locally.
	_, err := gitOutputBytes(ctx, repoRoot, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// globToRegexp converts a danger-name glob (only `*` is special; it matches any
// run of characters including `/`) into an anchored regexp.
func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	parts := strings.Split(pattern, "*")
	for i, part := range parts {
		b.WriteString(regexp.QuoteMeta(part))
		if i < len(parts)-1 {
			b.WriteString(".*")
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
