package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fulmenhq/limensafe/pkg/catalog"
	"github.com/fulmenhq/limensafe/pkg/coverage"
	"github.com/fulmenhq/limensafe/pkg/engine"
	"github.com/fulmenhq/limensafe/pkg/extractor"
	"github.com/fulmenhq/limensafe/pkg/output"
)

func TestAccountCoverageSkip(t *testing.T) {
	s := coverage.New("file", nil)
	accountCoverageSkip(&s, extractor.SkipEvent{Reason: extractor.SkipIgnored, CoverageGap: coverage.UnfollowedSymlink})
	accountCoverageSkip(&s, extractor.SkipEvent{Reason: extractor.SkipUnreadable, CoverageGap: coverage.UnreadableDirectory})
	accountCoverageSkip(&s, extractor.SkipEvent{Reason: extractor.SkipIgnored, IsDirectory: true, RepresentedFiles: 3})
	accountCoverageSkip(&s, extractor.SkipEvent{Reason: extractor.SkipReason(100)})
	if s.Status != "incomplete" || s.SkipsByReason["ignored"] != 3 || len(s.BlockingSkipsByReason) != 0 {
		t.Fatal(s)
	}
	for _, code := range []coverage.GapCode{coverage.UnfollowedSymlink, coverage.UnreadableDirectory, coverage.UnrecognizedSkipReason} {
		if s.NonBudgetableGapsByCode[code] != 1 {
			t.Fatal(s)
		}
	}
}

func TestAccountCoverageUnreadableFileRemainsBudgetable(t *testing.T) {
	s := coverage.New("file", map[string]int{"unreadable": 1})
	accountCoverageSkip(&s, extractor.SkipEvent{Reason: extractor.SkipUnreadable})
	if s.Status != "acknowledged" || s.BlockingSkipsByReason["unreadable"] != 1 || len(s.NonBudgetableGapsByCode) != 0 {
		t.Fatal(s)
	}
	accountCoverageSkip(&s, extractor.SkipEvent{Reason: extractor.SkipUnreadable, CoverageGap: coverage.UnknownEntry})
	if s.Status != "incomplete" || s.BlockingSkipsByReason["unreadable"] != 1 {
		t.Fatal(s)
	}
}

func TestAttestedChildOutputFailsClosed(t *testing.T) {
	out := output.Output{
		ScanMetadata: output.ScanMetadata{OutputSchemaVersion: output.SchemaVersion},
		Summary:      output.ScanSummary{Coverage: coverage.New("file", nil)},
	}
	valid, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeAttestedScan(valid); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*output.Output){
		func(o *output.Output) { o.ScanMetadata.OutputSchemaVersion = "99.0.0" },
		func(o *output.Output) { o.Summary.Coverage.Status = "incomplete" },
		func(o *output.Output) { o.Summary.Coverage.SkipsByReason = nil },
	} {
		candidate := out
		mutate(&candidate)
		data, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeAttestedScan(data); err == nil {
			t.Fatal("malformed child accepted")
		}
	}
	for _, data := range [][]byte{append(append([]byte{}, valid...), []byte("{}")...), []byte(`{"protected-raw-key":"private"}`)} {
		if _, err := decodeAttestedScan(data); err == nil || strings.Contains(err.Error(), "protected") {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestCoverageGateIndependentOfCriticalDetectorThreshold(t *testing.T) {
	cat, err := catalog.LoadFile("../../testdata/synthetic-acme/catalog/synthetic-acme.catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for i := range cat.Entities {
		cat.Entities[i].SeverityOverride = "medium"
	}
	scanner, err := engine.NewScannerWithOptions([]*catalog.Catalog{cat}, "public_oss", engine.ScannerOptions{BlockThreshold: "critical"})
	if err != nil {
		t.Fatal(err)
	}
	findings := toOutputFindings(scanner.ScanUnit(extractor.InputUnit{
		SourceID: "small.txt", SourceKind: "file", Content: []byte("acme"), Encoding: "utf-8",
	}))
	if len(findings) == 0 {
		t.Fatal("fixture must detect below-threshold content")
	}
	for _, f := range findings {
		if f.Decision == "block" {
			t.Fatal("fixture unexpectedly blocks")
		}
	}
	s := coverage.New("file", nil)
	s.AddSkip("file_too_large", 1, "")
	if scanGateResult(findings, s, "release") != ErrFindingsBlocked || scanGateResult(nil, s, "release") != ErrFindingsBlocked {
		t.Fatal("coverage failed to block independently")
	}
	if err := scanGateResult(findings, s, "ci"); err != nil {
		t.Fatal(err)
	}
	s = coverage.New("file", map[string]int{"file_too_large": 2})
	s.AddSkip("file_too_large", 1, "")
	if err := scanGateResult(findings, s, "release"); err != nil {
		t.Fatal(err)
	}
	cat.Entities[0].SeverityOverride = "critical"
	criticalScanner, err := engine.NewScannerWithOptions([]*catalog.Catalog{cat}, "public_oss", engine.ScannerOptions{BlockThreshold: "critical"})
	if err != nil {
		t.Fatal(err)
	}
	criticalFindings := toOutputFindings(criticalScanner.ScanUnit(extractor.InputUnit{
		SourceID: "small.txt", SourceKind: "file", Content: []byte("acme"), Encoding: "utf-8",
	}))
	foundCritical := false
	for _, f := range criticalFindings {
		if f.Severity == "critical" && f.Decision == "block" {
			foundCritical = true
		}
	}
	if !foundCritical {
		t.Fatal("fixture must contain an actual critical detection")
	}
	if scanGateResult(criticalFindings, coverage.New("file", nil), "release") != ErrFindingsBlocked || scanGateResult(criticalFindings, s, "release") != ErrFindingsBlocked {
		t.Fatal("good coverage cleared critical detection")
	}
}
