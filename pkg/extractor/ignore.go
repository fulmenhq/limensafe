package extractor

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	limensafeIgnoreFile = ".limensafeignore"
	gitIgnoreFile       = ".gitignore"
)

type ignoreRule struct {
	pattern       string
	negated       bool
	directoryOnly bool
	anchored      bool
	source        string
	line          int
}

// IgnoreMatcher evaluates root-level .gitignore and .limensafeignore rules.
// internal-brief intentionally does not compose nested ignore files; per-directory
// inheritance is deferred until the matcher grows beyond the v0.0.4 slice.
type IgnoreMatcher struct {
	rules []ignoreRule
}

// LoadRootIgnoreMatcher loads root-level ignore files, with .limensafeignore
// taking precedence after .gitignore because later rules refine earlier ones.
func LoadRootIgnoreMatcher(root string) (*IgnoreMatcher, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("ignore: abs root: %w", err)
	}

	var rules []ignoreRule
	for _, name := range []string{gitIgnoreFile, limensafeIgnoreFile} {
		loaded, err := loadIgnoreRules(filepath.Join(rootAbs, name), name)
		if err != nil {
			return nil, err
		}
		rules = append(rules, loaded...)
	}
	return &IgnoreMatcher{rules: rules}, nil
}

// Match reports whether relPath is ignored, and returns the rule that made
// the final decision. relPath must be relative to the scan root.
func (m *IgnoreMatcher) Match(relPath string, isDir bool) (bool, string) {
	if m == nil || len(m.rules) == 0 {
		return false, ""
	}
	rel := cleanIgnorePath(relPath)
	if rel == "." || rel == "" {
		return false, ""
	}

	ignored := false
	source := ""
	for _, rule := range m.rules {
		if !rule.matches(rel, isDir) {
			continue
		}
		ignored = !rule.negated
		source = fmt.Sprintf("%s:%d", rule.source, rule.line)
	}
	return ignored, source
}

// MayReincludeUnder reports whether a later negated rule could re-include a
// descendant of relDir. Callers use this to avoid pruning a directory before
// all descendant paths can be evaluated.
func (m *IgnoreMatcher) MayReincludeUnder(relDir string) bool {
	if m == nil || len(m.rules) == 0 {
		return false
	}
	rel := cleanIgnorePath(relDir)
	if rel == "." || rel == "" {
		return false
	}
	prefix := rel + "/"
	for _, rule := range m.rules {
		if !rule.negated {
			continue
		}
		if !rule.anchored && !strings.Contains(rule.pattern, "/") {
			return true
		}
		if strings.HasPrefix(rule.pattern, prefix) || strings.Contains(rule.pattern, "**") {
			return true
		}
	}
	return false
}

func loadIgnoreRules(pathOnDisk, source string) ([]ignoreRule, error) {
	file, err := os.Open(pathOnDisk)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("ignore: open %s: %w", source, err)
	}
	defer func() { _ = file.Close() }()

	var rules []ignoreRule
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		rule, ok := parseIgnoreRule(scanner.Text(), source, lineNo)
		if ok {
			rules = append(rules, rule)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ignore: read %s: %w", source, err)
	}
	return rules, nil
}

func parseIgnoreRule(line, source string, lineNo int) (ignoreRule, bool) {
	raw := strings.TrimSpace(line)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return ignoreRule{}, false
	}
	if strings.HasPrefix(raw, `\#`) || strings.HasPrefix(raw, `\!`) {
		raw = raw[1:]
	}

	negated := false
	if strings.HasPrefix(raw, "!") {
		negated = true
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "!"))
	}
	if raw == "" {
		return ignoreRule{}, false
	}

	directoryOnly := strings.HasSuffix(raw, "/")
	raw = strings.TrimRight(raw, "/")
	anchored := strings.HasPrefix(raw, "/")
	raw = strings.TrimLeft(raw, "/")
	raw = cleanIgnorePath(raw)
	if raw == "." || raw == "" {
		return ignoreRule{}, false
	}

	return ignoreRule{
		pattern:       raw,
		negated:       negated,
		directoryOnly: directoryOnly,
		anchored:      anchored,
		source:        source,
		line:          lineNo,
	}, true
}

func (r ignoreRule) matches(rel string, isDir bool) bool {
	if r.directoryOnly {
		return r.matchesDirectory(rel, isDir)
	}
	return r.matchesPath(rel)
}

func (r ignoreRule) matchesDirectory(rel string, isDir bool) bool {
	pattern := r.pattern
	if r.anchored || strings.Contains(pattern, "/") {
		return rel == pattern || strings.HasPrefix(rel, pattern+"/") || matchGlob(pattern, rel)
	}

	if isDir && matchGlob(pattern, path.Base(rel)) {
		return true
	}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if !matchGlob(pattern, part) {
			continue
		}
		if i < len(parts)-1 || isDir {
			return true
		}
	}
	return false
}

func (r ignoreRule) matchesPath(rel string) bool {
	pattern := r.pattern
	if r.anchored || strings.Contains(pattern, "/") {
		return matchGlob(pattern, rel)
	}
	return matchGlob(pattern, path.Base(rel))
}

func matchGlob(pattern, rel string) bool {
	ok, err := doublestar.PathMatch(pattern, rel)
	return err == nil && ok
}

func cleanIgnorePath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	p = strings.TrimPrefix(p, "./")
	if p == "" {
		return ""
	}
	return path.Clean(p)
}
