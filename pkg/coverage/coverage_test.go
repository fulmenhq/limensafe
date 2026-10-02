package coverage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseAllowances(t *testing.T) {
	for _, values := range [][]string{{"other=1"}, {"ignored=1"}, {"unknown=-1"}, {"unknown=+1"}, {"unknown=1.2"}, {"unknown="}, {"unknown=2147483648"}, {"unknown=1", "unknown=2"}} {
		if _, err := ParseAllowances(values); err == nil {
			t.Errorf("accepted invalid allowance %v", values)
		}
	}
	a, err := ParseAllowances([]string{"unknown=0", "file_too_large=2"})
	if err != nil || len(a) != 2 || a["file_too_large"] != 2 {
		t.Fatalf("allowances=%v err=%v", a, err)
	}
}

func TestSummaryStates(t *testing.T) {
	allowances := map[string]int{"file_too_large": 2}
	s := New("file", allowances)
	allowances["file_too_large"] = 100
	s.AddSkip("ignored", 3, "")
	if s.Status != "complete" {
		t.Fatal(s)
	}
	s.AddSkip("file_too_large", 1, "")
	if s.Status != "acknowledged" || len(s.UnacknowledgedSkipsByReason) != 0 {
		t.Fatal(s)
	}
	s.AddSkip("file_too_large", 2, "")
	if s.Status != "incomplete" || s.UnacknowledgedSkipsByReason["file_too_large"] != 1 {
		t.Fatal(s)
	}
	s.AddSkip("unreadable", 1, "")
	if s.UnacknowledgedSkipsByReason["unreadable"] != 1 {
		t.Fatal(s)
	}
}

func TestGapsAreNotSkipUnits(t *testing.T) {
	for _, gap := range []GapCode{UnreadableDirectory, UnknownEntry, UnfollowedSymlink, RefMissingLocalObjects, PrimaryBaselineUnscanned, NamesOnlyContentOmitted} {
		s := New("unique_blob", map[string]int{"unreadable": 100})
		s.AddSkip("unreadable", 1, gap)
		if s.Status != "incomplete" || len(s.SkipsByReason) != 0 || s.NonBudgetableGapsByCode[gap] != 1 {
			t.Fatal(s)
		}
	}
	s := New("file", nil)
	s.AddSkip("protected-raw-reason", 1, "")
	s.AddGap("protected-raw-gap")
	b, err := json.Marshal(s)
	if err != nil || strings.Contains(string(b), "protected") || strings.Contains(string(b), "null") {
		t.Fatalf("%s %v", b, err)
	}
	if s.NonBudgetableGapsByCode[UnrecognizedSkipReason] != 1 || s.NonBudgetableGapsByCode[UnknownEntry] != 1 {
		t.Fatal(s)
	}
}

func TestValidateMapStatusConsistency(t *testing.T) {
	newValid := func() Summary {
		s := New("file", map[string]int{"file_too_large": 2})
		s.AddSkip("file_too_large", 1, "")
		return s
	}
	if err := newValid().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Summary){
		func(s *Summary) { s.Status = "complete" },
		func(s *Summary) { s.BlockingSkipsByReason["file_too_large"] = 0 },
		func(s *Summary) { s.UnacknowledgedSkipsByReason["file_too_large"] = 1 },
		func(s *Summary) { s.AllowancesByReason["ignored"] = 1 },
		func(s *Summary) { s.SkipsByReason = nil },
		func(s *Summary) { s.NonBudgetableGapsByCode["raw-private-code"] = 1 },
	} {
		s := newValid()
		mutate(&s)
		if err := s.Validate(); err == nil {
			t.Fatal(s)
		}
	}
}
