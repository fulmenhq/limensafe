package extractor

import (
	"path/filepath"
	"testing"
)

func TestIgnoreMatcher_RootPatterns(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, ".limensafeignore"), `
# comments and blanks are ignored
/anchored.txt
generated/
**/fixtures/*.secret
*.log
docs/**
!docs/keep.md
`)

	m, err := LoadRootIgnoreMatcher(tmp)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path   string
		isDir  bool
		ignore bool
	}{
		{"anchored.txt", false, true},
		{"sub/anchored.txt", false, false},
		{"generated", true, true},
		{"generated/out.txt", false, true},
		{"sub/generated/out.txt", false, true},
		{"test/fixtures/token.secret", false, true},
		{"notes/error.log", false, true},
		{"docs/drop.md", false, true},
		{"docs/keep.md", false, false},
		{"src/keep.go", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got, _ := m.Match(tc.path, tc.isDir)
			if got != tc.ignore {
				t.Fatalf("Match(%q) = %v, want %v", tc.path, got, tc.ignore)
			}
		})
	}
}

func TestIgnoreMatcher_GitignorePrecedence(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, ".gitignore"), "dist/\n")
	mustWrite(t, filepath.Join(tmp, ".limensafeignore"), "!dist/keep.txt\n")

	m, err := LoadRootIgnoreMatcher(tmp)
	if err != nil {
		t.Fatal(err)
	}

	if ignored, _ := m.Match("dist/drop.txt", false); !ignored {
		t.Fatal("expected .gitignore dist/ to ignore dist/drop.txt")
	}
	if ignored, _ := m.Match("dist/keep.txt", false); ignored {
		t.Fatal("expected .limensafeignore negation to re-include dist/keep.txt")
	}
}
