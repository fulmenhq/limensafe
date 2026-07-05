package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fulmenhq/limensafe/pkg/catalog"
)

// captureStderr redirects os.Stderr for the duration of fn and returns what was
// written. Tests here do not run in parallel, so swapping the global is safe.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// setCatalogBuildFlags sets the package-level catalog-build flag vars to the
// defaults a fresh command invocation would carry, then applies overrides.
// Globals persist across tests, so each test sets every field it depends on.
func setCatalogBuildFlags(fromTermList, out, id, class, severity string, noWholeWord bool) {
	catalogBuildFromTermList = fromTermList
	catalogBuildOut = out
	catalogBuildID = id
	catalogBuildClass = class
	catalogBuildSeverity = severity
	catalogBuildNoWholeWord = noWholeWord
}

func writeTermListFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "terms.txt")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write term-list fixture: %v", err)
	}
	return path
}

func TestCatalogBuild_HappyPathWritesValidCatalog(t *testing.T) {
	src := writeTermListFile(t, "AcmeCorp==>ClientAlpha\nAcme==>ClientAlpha\nHorizon==>ProjectBeta\n")
	out := filepath.Join(t.TempDir(), "catalog.yaml")
	setCatalogBuildFlags(src, out, "tl-happy", "codename", "", false)

	if err := runCatalogBuild(nil, nil); err != nil {
		t.Fatalf("build: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	c, err := catalog.LoadBytes(data)
	if err != nil {
		t.Fatalf("generated catalog failed to load: %v", err)
	}
	if len(c.Entities) != 2 {
		t.Fatalf("entities = %d, want 2", len(c.Entities))
	}
	for _, e := range c.Entities {
		if !e.Variants.WholeWord {
			t.Fatalf("whole_word should default on, entity %s", e.ID)
		}
	}
}

func TestCatalogBuild_MissingFromTermListIsConfigError(t *testing.T) {
	setCatalogBuildFlags("", "/tmp/unused.yaml", "id", "codename", "", false)
	err := runCatalogBuild(nil, nil)
	if !errors.Is(err, ErrConfigInvalid) {
		t.Fatalf("want ErrConfigInvalid, got: %v", err)
	}
}

func TestCatalogBuild_MissingOutIsConfigError(t *testing.T) {
	src := writeTermListFile(t, "Acme==>ClientAlpha\n")
	setCatalogBuildFlags(src, "", "id", "codename", "", false)
	err := runCatalogBuild(nil, nil)
	if !errors.Is(err, ErrConfigInvalid) {
		t.Fatalf("want ErrConfigInvalid, got: %v", err)
	}
}

func TestCatalogBuild_MissingCatalogIDIsConfigError(t *testing.T) {
	src := writeTermListFile(t, "Acme==>ClientAlpha\n")
	setCatalogBuildFlags(src, filepath.Join(t.TempDir(), "c.yaml"), "", "codename", "", false)
	err := runCatalogBuild(nil, nil)
	if !errors.Is(err, ErrConfigInvalid) {
		t.Fatalf("want ErrConfigInvalid, got: %v", err)
	}
}

// A malformed term-list classifies as a config error (exit 2) and the
// diagnostic must not echo the protected content on the offending line.
func TestCatalogBuild_MalformedInputIsRedactionSafeConfigError(t *testing.T) {
	const protected = "SUPERSECRETCODENAME"
	src := writeTermListFile(t, "Acme==>ClientAlpha\n"+protected+" has no separator\n")
	out := filepath.Join(t.TempDir(), "c.yaml")
	setCatalogBuildFlags(src, out, "tl-bad", "codename", "", false)

	err := runCatalogBuild(nil, nil)
	if !errors.Is(err, ErrConfigInvalid) {
		t.Fatalf("want ErrConfigInvalid, got: %v", err)
	}
	if strings.Contains(err.Error(), protected) || strings.Contains(err.Error(), strings.ToLower(protected)) {
		t.Fatalf("error leaked protected content: %v", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected line-numbered diagnostic, got: %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("output file must not be written on a failed build")
	}
}

func TestCatalogBuild_InvalidRegexIsRedactionSafeConfigError(t *testing.T) {
	const protected = "SUPERSECRETCODENAME"
	src := writeTermListFile(t, "Acme==>ClientAlpha\nregex:("+protected+"\n")
	out := filepath.Join(t.TempDir(), "c.yaml")
	setCatalogBuildFlags(src, out, "tl-bad-regex", "codename", "", false)

	err := runCatalogBuild(nil, nil)
	if !errors.Is(err, ErrConfigInvalid) {
		t.Fatalf("want ErrConfigInvalid, got: %v", err)
	}
	if strings.Contains(err.Error(), protected) || strings.Contains(err.Error(), strings.ToLower(protected)) {
		t.Fatalf("error leaked protected regex content: %v", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected line-numbered diagnostic, got: %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("output file must not be written on a failed build")
	}
}

func TestCatalogBuild_StructuredInputWritesValidCatalog(t *testing.T) {
	src := writeTermListFile(t, `
Acme==>ClientAlpha # class=client_identity severity=high
regex:\bHRZN-[0-9]{4}\b # class=operational_pattern severity=critical
allowlist:literal:HRZN-0000 # case_insensitive=true whole_word=true
`)
	out := filepath.Join(t.TempDir(), "catalog.yaml")
	setCatalogBuildFlags(src, out, "tl-structured-cli", "codename", "", false)

	if err := runCatalogBuild(nil, nil); err != nil {
		t.Fatalf("build: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	c, err := catalog.LoadBytes(data)
	if err != nil {
		t.Fatalf("generated catalog failed to load: %v", err)
	}
	if len(c.Entities) != 2 {
		t.Fatalf("entities = %d, want literal + regex", len(c.Entities))
	}
	if len(c.Allowlist) != 1 {
		t.Fatalf("allowlist len = %d, want 1", len(c.Allowlist))
	}
}

func TestCatalogBuild_MissingInputFileIsRuntimeError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.txt")
	setCatalogBuildFlags(missing, filepath.Join(t.TempDir(), "c.yaml"), "id", "codename", "", false)
	err := runCatalogBuild(nil, nil)
	if !errors.Is(err, ErrRuntime) {
		t.Fatalf("want ErrRuntime, got: %v", err)
	}
}

// The --from-termlist path is operator-controlled and may embed a protected
// name; a read failure must not echo it.
func TestCatalogBuild_MissingInputPathNotLeaked(t *testing.T) {
	const sentinel = "SUPERSECRETCLIENT"
	missing := filepath.Join(t.TempDir(), sentinel+"-terms.txt")
	setCatalogBuildFlags(missing, filepath.Join(t.TempDir(), "c.yaml"), "id", "codename", "", false)
	err := runCatalogBuild(nil, nil)
	if !errors.Is(err, ErrRuntime) {
		t.Fatalf("want ErrRuntime, got: %v", err)
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("error leaked the operator-controlled input path: %v", err)
	}
}

// The --out path is likewise operator-controlled; the success line must report
// a count only, never the path.
func TestCatalogBuild_SuccessDoesNotLeakOutputPath(t *testing.T) {
	const sentinel = "SUPERSECRETCLIENT"
	src := writeTermListFile(t, "Acme==>ClientAlpha\n")
	outDir := filepath.Join(t.TempDir(), sentinel+"-out")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	out := filepath.Join(outDir, sentinel+".catalog.yaml")
	setCatalogBuildFlags(src, out, "tl-leak", "codename", "", false)

	var buildErr error
	stderr := captureStderr(t, func() { buildErr = runCatalogBuild(nil, nil) })
	if buildErr != nil {
		t.Fatalf("build: %v", buildErr)
	}
	if strings.Contains(stderr, sentinel) {
		t.Fatalf("success stderr leaked the output path: %q", stderr)
	}
	if !strings.Contains(stderr, "catalog written") {
		t.Fatalf("expected a success message on stderr, got: %q", stderr)
	}
}

// A newly-created catalog holds protected vocabulary and must not be
// group/world-readable.
func TestCatalogBuild_OutputFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file-mode semantics")
	}
	src := writeTermListFile(t, "Acme==>ClientAlpha\n")
	out := filepath.Join(t.TempDir(), "c.yaml")
	setCatalogBuildFlags(src, out, "tl-mode", "codename", "", false)
	if err := runCatalogBuild(nil, nil); err != nil {
		t.Fatalf("build: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("output mode = %o, want no group/world bits (0600)", perm)
	}
}

func TestCatalogBuild_NoWholeWordOptOut(t *testing.T) {
	src := writeTermListFile(t, "Acme==>ClientAlpha\n")
	out := filepath.Join(t.TempDir(), "c.yaml")
	setCatalogBuildFlags(src, out, "tl-nww", "codename", "", true)

	if err := runCatalogBuild(nil, nil); err != nil {
		t.Fatalf("build: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	c, err := catalog.LoadBytes(data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Entities[0].Variants.WholeWord {
		t.Fatal("--no-whole-word should produce whole_word: false")
	}
}
