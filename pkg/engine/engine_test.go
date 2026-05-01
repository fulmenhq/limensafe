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
