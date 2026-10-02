package extractor

import (
	"context"
	"testing"
)

func newCoverageBlobRepo(t *testing.T, mixed bool) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "acme.bin", "shared clean bytes\n")
	writeFile(t, dir, "horizon.bin", "shared clean bytes\n")
	if mixed {
		writeFile(t, dir, "safe.txt", "shared clean bytes\n")
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "fixture")
	git(t, dir, "branch", "other")
	return dir
}

func TestHistoryCoverageUniqueBlobWithBinaryPaths(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		dir := newCoverageBlobRepo(t, mixed)
		e, err := NewGitHistoryExtractor(dir, GitHistoryOptions{IncludeBlobs: true})
		if err != nil {
			t.Fatal(err)
		}
		units, skips, err := collectRun(t, e)
		if err != nil {
			t.Fatal(err)
		}
		coverageSkips, legacySkips, pathAttributions := 0, 0, 0
		for _, skip := range skips {
			if !skip.CoverageExcluded {
				coverageSkips++
			}
			if !skip.LegacyExcluded {
				legacySkips++
			}
			pathAttributions += len(skip.GitBlobAttributions)
		}
		if legacySkips != 2 || pathAttributions != 2 {
			t.Fatalf("legacy=%d paths=%d", legacySkips, pathAttributions)
		}
		if mixed {
			if len(units) != 1 || coverageSkips != 0 || len(units[0].GitBlobAttributions) != 1 {
				t.Fatalf("mixed units=%v skips=%v", units, skips)
			}
		} else if len(units) != 0 || coverageSkips != 1 {
			t.Fatalf("binary units=%v skips=%v", units, skips)
		}
	}
}

func TestPublishCoverageRetainsAllPathsWithOneBlobUnit(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		dir := newCoverageBlobRepo(t, mixed)
		ctx := context.Background()
		refs, err := EnumerateRefs(ctx, dir, RefEnumerationOptions{IncludeLocal: true, Branches: true, Tags: true})
		if err != nil {
			t.Fatal(err)
		}
		e, err := NewPublishSurfaceExtractor(dir, refs, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.Enumerate(ctx); err != nil {
			t.Fatal(err)
		}
		blobs := e.SkippedBlobs()
		if mixed {
			blobs = e.Blobs()
			if len(e.Skips()) != 0 {
				t.Fatal(e.Skips())
			}
		} else if len(e.Skips()) != 1 {
			t.Fatal(e.Skips())
		}
		if len(blobs) != 1 || len(blobs[0].Refs) != 2 {
			t.Fatal(blobs)
		}
		wantPaths := 2
		if mixed {
			wantPaths = 3
		}
		if len(blobs[0].Paths) != wantPaths {
			t.Fatal(blobs)
		}
	}
}
