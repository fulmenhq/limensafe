package engine

import (
	"testing"

	"github.com/fulmenhq/limensafe/pkg/catalog"
	"github.com/fulmenhq/limensafe/pkg/extractor"
)

func syntheticScanner(t *testing.T) *Scanner {
	t.Helper()
	c, err := catalog.LoadFile("../../testdata/synthetic-acme/catalog/synthetic-acme.catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewScanner([]*catalog.Catalog{c}, "public_oss")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestScanUnitDetectsLiteral(t *testing.T) {
	s := syntheticScanner(t)
	findings := s.ScanUnit(extractor.InputUnit{
		SourceID:   "internal/cmd/profile_test.go",
		SourceKind: "file",
		Content:    []byte("profile acme-dev\n"),
		Encoding:   "utf-8",
	})

	if len(findings) == 0 {
		t.Fatal("expected findings")
	}
	if findings[0].EntityID != "e-client-1" {
		t.Fatalf("entity id = %q, want e-client-1", findings[0].EntityID)
	}
	if findings[0].Line != 1 || findings[0].Column != 9 {
		t.Fatalf("location = %d:%d, want 1:9", findings[0].Line, findings[0].Column)
	}
}

func TestScanUnitDetectsPathSegment(t *testing.T) {
	s := syntheticScanner(t)
	findings := s.ScanUnit(extractor.InputUnit{
		SourceID:   "internal/clients/acme/data.go",
		SourceKind: "file",
		Content:    []byte("package data\n"),
		Encoding:   "utf-8",
	})

	found := false
	for _, f := range findings {
		if f.Surface == SurfacePath && f.DetectorID == DetectorPathSegment && f.EntityID == "e-client-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected path-segment finding, got %#v", findings)
	}
}

func TestScanUnitCoOccurrence(t *testing.T) {
	s := syntheticScanner(t)
	findings := s.ScanUnit(extractor.InputUnit{
		SourceID:   "internal/doctor/redact_test.go",
		SourceKind: "file",
		Content:    []byte("selected_profile: acme-horizon-dev\n"),
		Encoding:   "utf-8",
	})

	found := false
	for _, f := range findings {
		if f.DetectorID == DetectorCoOccurrence && f.RuleID == "r-cooccur-1" && f.Severity == "critical" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected co-occurrence finding, got %#v", findings)
	}
}

func TestScanUnitBranchSurface(t *testing.T) {
	s := syntheticScanner(t)
	findings := s.ScanUnit(extractor.InputUnit{
		SourceID:   "branch_name",
		SourceKind: "branch_name",
		Content:    []byte("feat/acme-redash-fix\n"),
		Encoding:   "utf-8",
	})
	if len(findings) == 0 {
		t.Fatal("expected branch finding")
	}
	if findings[0].Surface != SurfaceBranch {
		t.Fatalf("surface = %q, want %q", findings[0].Surface, SurfaceBranch)
	}
}

func TestScanUnitAllowedVisibilitySuppressesEntity(t *testing.T) {
	c, err := catalog.LoadFile("../../testdata/synthetic-acme/catalog/synthetic-acme.catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewScanner([]*catalog.Catalog{c}, "engagement_private")
	if err != nil {
		t.Fatal(err)
	}
	findings := s.ScanUnit(extractor.InputUnit{
		SourceID:   "docs/usage.md",
		SourceKind: "file",
		Content:    []byte("use tilden for private engagement fixtures\n"),
		Encoding:   "utf-8",
	})
	for _, f := range findings {
		if f.EntityID == "e-codename-2" {
			t.Fatalf("sanctioned codename should be allowed in engagement_private, got %#v", findings)
		}
	}
}

func TestScanUnitBlockThresholdMedium(t *testing.T) {
	c := catalogFromYAML(t, `
catalog_id: threshold-test
schema_version: "1.0.0"
default_severity: medium
entities:
  - id: e-threshold-1
    class: operational_pattern
    aliases: ["THRESHOLD_ALIAS"]
`)
	s, err := NewScannerWithOptions([]*catalog.Catalog{c}, "public_oss", ScannerOptions{
		BlockThreshold: "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	findings := s.ScanUnit(extractor.InputUnit{
		SourceID:   "threshold.txt",
		SourceKind: "file",
		Content:    []byte("THRESHOLD_ALIAS\n"),
		Encoding:   "utf-8",
	})
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %#v", findings)
	}
	if findings[0].Decision != DecisionBlock {
		t.Fatalf("decision = %q, want block", findings[0].Decision)
	}
}

func TestScanUnitWholeWordSuppressesSubstringFalsePositives(t *testing.T) {
	s := scannerFromCatalogYAML(t, `
catalog_id: test-catalog
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-format-1
    class: operational_pattern
    aliases: ["ILT"]
    variants:
      whole_word: true
`)

	for _, content := range []string{"built", "split", "splittable", "rebuilt", "tilt"} {
		t.Run(content, func(t *testing.T) {
			findings := s.ScanUnit(extractor.InputUnit{
				SourceID:   "go.sum",
				SourceKind: "file",
				Content:    []byte(content),
				Encoding:   "utf-8",
			})
			if len(findings) != 0 {
				t.Fatalf("expected no findings for substring %q, got %#v", content, findings)
			}
		})
	}
}

func TestScanUnitWholeWordMatchesBoundedTokens(t *testing.T) {
	s := scannerFromCatalogYAML(t, `
catalog_id: test-catalog
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-format-1
    class: operational_pattern
    aliases: ["ILT"]
    variants:
      whole_word: true
`)

	for _, content := range []string{" ILT ", ",ILT,", "ILT\n", "ILT", "prefix:ILT"} {
		t.Run(content, func(t *testing.T) {
			findings := s.ScanUnit(extractor.InputUnit{
				SourceID:   "fixture.txt",
				SourceKind: "file",
				Content:    []byte(content),
				Encoding:   "utf-8",
			})
			if len(findings) == 0 {
				t.Fatalf("expected whole-word finding for %q", content)
			}
			if findings[0].EntityID != "e-format-1" {
				t.Fatalf("entity id = %q, want e-format-1", findings[0].EntityID)
			}
		})
	}
}

func TestScanUnitLongAliasDefaultsToSubstringCompatibility(t *testing.T) {
	s := scannerFromCatalogYAML(t, `
catalog_id: test-catalog
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-client-1
    class: client_identity
    aliases: ["Acme Corp"]
`)

	findings := s.ScanUnit(extractor.InputUnit{
		SourceID:   "docs/example.md",
		SourceKind: "file",
		Content:    []byte("prefixAcme Corpsuffix"),
		Encoding:   "utf-8",
	})
	if len(findings) == 0 {
		t.Fatal("expected substring finding for long alias without whole_word")
	}
}

func TestScanUnitShortAliasDefaultsToWholeWordWithExplicitOptOut(t *testing.T) {
	t.Run("auto whole-word", func(t *testing.T) {
		s := scannerFromCatalogYAML(t, `
catalog_id: test-catalog
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-format-1
    class: operational_pattern
    aliases: ["ILT"]
`)
		findings := s.ScanUnit(extractor.InputUnit{
			SourceID:   "lockfile.txt",
			SourceKind: "file",
			Content:    []byte("built"),
			Encoding:   "utf-8",
		})
		if len(findings) != 0 {
			t.Fatalf("expected short alias to default whole-word, got %#v", findings)
		}
	})

	t.Run("explicit substring opt-out", func(t *testing.T) {
		s := scannerFromCatalogYAML(t, `
catalog_id: test-catalog
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-format-1
    class: operational_pattern
    aliases: ["ILT"]
    variants:
      whole_word: false
      case_insensitive: true
`)
		findings := s.ScanUnit(extractor.InputUnit{
			SourceID:   "lockfile.txt",
			SourceKind: "file",
			Content:    []byte("built"),
			Encoding:   "utf-8",
		})
		if len(findings) == 0 {
			t.Fatal("expected explicit whole_word=false to preserve substring matching")
		}
	})
}

func TestScanUnitWholeWordSuppressesCoOccurrenceFromSubstring(t *testing.T) {
	s := scannerFromCatalogYAML(t, `
catalog_id: test-catalog
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-format-1
    class: operational_pattern
    aliases: ["ILT"]
    variants:
      whole_word: true
      case_insensitive: true
  - id: e-codename-1
    class: codename
    aliases: ["horizon"]
co_occurrence_rules:
  - rule_id: r-cooccur-1
    terms: [e-format-1, e-codename-1]
    window_kind: file
    severity_override: critical
`)

	findings := s.ScanUnit(extractor.InputUnit{
		SourceID:   "docs/example.md",
		SourceKind: "file",
		Content:    []byte("built near horizon"),
		Encoding:   "utf-8",
	})
	for _, f := range findings {
		if f.DetectorID == DetectorCoOccurrence {
			t.Fatalf("substring match must not feed co-occurrence, got %#v", findings)
		}
	}
}

func TestScanUnitSyntheticAcmeWholeWordFixtureHasZeroAcronymFindings(t *testing.T) {
	s := syntheticScanner(t)
	findings := s.ScanUnit(extractor.InputUnit{
		SourceID:   "internal/locks/sha_noise.txt",
		SourceKind: "file",
		Content:    []byte("built split splittable rebuilt tilt InfoMod LbkRs+hbI=\n"),
		Encoding:   "utf-8",
	})
	for _, f := range findings {
		if f.EntityID == "e-format-1" {
			t.Fatalf("expected synthetic short-acronym fixture to produce zero acronym findings, got %#v", findings)
		}
	}
}

func scannerFromCatalogYAML(t *testing.T, data string) *Scanner {
	t.Helper()
	c := catalogFromYAML(t, data)
	s, err := NewScanner([]*catalog.Catalog{c}, "public_oss")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func catalogFromYAML(t *testing.T, data string) *catalog.Catalog {
	t.Helper()
	c, err := catalog.LoadBytes([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
