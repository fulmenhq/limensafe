package extractor

import "testing"

func TestMatchesDangerPattern_Defaults(t *testing.T) {
	cases := []struct {
		name      string
		wantMatch bool
		wantPat   string
	}{
		{"backup/old", true, "backup/*"},
		{"backup/main-2026", true, "backup/*"},
		{"main-pre-rewrite-2026-05-14", true, "*pre-rewrite*"},
		// `*pre-rewrite*` must match across a '/' segment boundary.
		{"backup/main-pre-rewrite-2026", true, "backup/*"},
		{"release-2026-bak", true, "*-bak"},
		{"wip/experiment", true, "wip/*"},
		{"main-snapshot-2026", true, "*-snapshot-*"},
		{"archive/v0", true, "archive/*"},
		{"main", false, ""},
		{"release/v0.1.0", false, ""},
		{"feature/wip-but-not-prefixed", false, ""},
	}
	for _, tc := range cases {
		pat, ok := MatchesDangerPattern(tc.name, DefaultDangerPatterns)
		if ok != tc.wantMatch {
			t.Errorf("%q: match=%v, want %v (pattern %q)", tc.name, ok, tc.wantMatch, pat)
			continue
		}
		if ok && pat != tc.wantPat {
			t.Errorf("%q: matched %q, want %q", tc.name, pat, tc.wantPat)
		}
	}
}

func TestGlobToRegexp_AnchoredNoStar(t *testing.T) {
	re, err := globToRegexp("main")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !re.MatchString("main") {
		t.Fatal("exact pattern should match itself")
	}
	if re.MatchString("maintenance") || re.MatchString("premain") {
		t.Fatal("no-star pattern must be fully anchored")
	}
}

func TestClassifyRef(t *testing.T) {
	cases := []struct {
		full string
		kind string
		name string
		ok   bool
	}{
		{"refs/heads/main", RefKindBranch, "main", true},
		{"refs/heads/backup/old", RefKindBranch, "backup/old", true},
		{"refs/tags/v0.1.0", RefKindTag, "v0.1.0", true},
		{"refs/remotes/origin/main", "", "", false},
		{"HEAD", "", "", false},
	}
	for _, tc := range cases {
		kind, name, ok := classifyRef(tc.full)
		if ok != tc.ok || kind != tc.kind || name != tc.name {
			t.Errorf("classifyRef(%q) = (%q,%q,%v), want (%q,%q,%v)", tc.full, kind, name, ok, tc.kind, tc.name, tc.ok)
		}
	}
}

func TestSplitLSRemoteLine(t *testing.T) {
	sha, full, ok := splitLSRemoteLine("abc123\trefs/heads/main")
	if !ok || sha != "abc123" || full != "refs/heads/main" {
		t.Fatalf("got (%q,%q,%v)", sha, full, ok)
	}
	if _, _, ok := splitLSRemoteLine("no-tab-here"); ok {
		t.Fatal("line without tab should not parse")
	}
}
