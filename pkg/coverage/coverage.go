// Package coverage accounts for selected-scope skips separately from detection
// and legacy extractor counters. Only closed reason and gap names reach output.
package coverage

import (
	"fmt"
	"strconv"
	"strings"
)

type GapCode string

const (
	UnreadableDirectory      GapCode = "unreadable_directory"
	UnknownEntry             GapCode = "unknown_entry"
	UnfollowedSymlink        GapCode = "unfollowed_symlink"
	UnrecognizedSkipReason   GapCode = "unrecognized_skip_reason"
	RefMissingLocalObjects   GapCode = "ref_missing_local_objects"
	PrimaryBaselineUnscanned GapCode = "primary_baseline_unscanned"
	NamesOnlyContentOmitted  GapCode = "names_only_content_omitted"
)

// Summary is the versioned coverage view. Gap events never count as files or
// blobs. A complete verdict describes selected scope after intentional exclusions.
type Summary struct {
	Status                      string          `json:"status"`
	SkipUnit                    string          `json:"skip_unit"`
	SkipsByReason               map[string]int  `json:"skips_by_reason"`
	BlockingSkipsByReason       map[string]int  `json:"blocking_skips_by_reason"`
	AllowancesByReason          map[string]int  `json:"allowances_by_reason"`
	UnacknowledgedSkipsByReason map[string]int  `json:"unacknowledged_skips_by_reason"`
	NonBudgetableGapsByCode     map[GapCode]int `json:"non_budgetable_gaps_by_code"`
}

func blockingReason(reason string) bool {
	switch reason {
	case "file_too_large", "binary_detected", "unreadable", "unknown":
		return true
	default:
		return false
	}
}

// ParseAllowances validates the entire input before extraction in every mode.
// Counts use a portable signed 32-bit range, independent of host architecture.
func ParseAllowances(values []string) (map[string]int, error) {
	result := map[string]int{}
	for _, value := range values {
		reason, count, ok := strings.Cut(value, "=")
		if !ok || !blockingReason(reason) {
			return nil, fmt.Errorf("skip allowance must name a blocking reason and nonnegative count")
		}
		if _, duplicate := result[reason]; duplicate {
			return nil, fmt.Errorf("duplicate skip allowance reason")
		}
		if count == "" || strings.IndexFunc(count, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return nil, fmt.Errorf("skip allowance count must be a nonnegative integer")
		}
		n, err := strconv.ParseInt(count, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("skip allowance count exceeds supported range")
		}
		result[reason] = int(n)
	}
	return result, nil
}

// New copies validated allowances so callers cannot mutate the verdict inputs.
func New(unit string, allowances map[string]int) Summary {
	s := Summary{
		Status: "complete", SkipUnit: unit,
		SkipsByReason: map[string]int{}, BlockingSkipsByReason: map[string]int{},
		AllowancesByReason: map[string]int{}, UnacknowledgedSkipsByReason: map[string]int{},
		NonBudgetableGapsByCode: map[GapCode]int{},
	}
	for reason, count := range allowances {
		s.AllowancesByReason[reason] = count
	}
	return s
}

// AddGap folds an unknown code into a closed fail-closed event, never raw text.
func (s *Summary) AddGap(code GapCode) {
	switch code {
	case UnreadableDirectory, UnknownEntry, UnfollowedSymlink, UnrecognizedSkipReason,
		RefMissingLocalObjects, PrimaryBaselineUnscanned, NamesOnlyContentOmitted:
	default:
		code = UnknownEntry
	}
	s.NonBudgetableGapsByCode[code]++
	s.recompute()
}

// AddSkip accepts known-cardinality skips only. Explicit gaps take precedence
// over legacy reasons; an unrecognized reason cannot become budgetable unknown.
func (s *Summary) AddSkip(reason string, units int, gap GapCode) {
	if gap != "" {
		s.AddGap(gap)
		return
	}
	if !blockingReason(reason) && reason != "ignored" {
		s.AddGap(UnrecognizedSkipReason)
		return
	}
	if units < 0 {
		s.AddGap(UnknownEntry)
		return
	}
	if units == 0 {
		return
	}
	s.SkipsByReason[reason] += units
	if blockingReason(reason) {
		s.BlockingSkipsByReason[reason] += units
	}
	s.recompute()
}

func (s *Summary) recompute() {
	s.Status = "complete"
	s.UnacknowledgedSkipsByReason = map[string]int{}
	for reason, actual := range s.BlockingSkipsByReason {
		if actual > 0 && s.Status == "complete" {
			s.Status = "acknowledged"
		}
		if remainder := actual - s.AllowancesByReason[reason]; remainder > 0 {
			s.UnacknowledgedSkipsByReason[reason] = remainder
			s.Status = "incomplete"
		}
	}
	for _, count := range s.NonBudgetableGapsByCode {
		if count > 0 {
			s.Status = "incomplete"
		}
	}
}

// Validate rejects malformed or internally inconsistent versioned coverage.
// Errors never echo caller-controlled map keys or values.
func (s Summary) Validate() error {
	if s.SkipUnit != "file" && s.SkipUnit != "unique_blob" {
		return fmt.Errorf("invalid coverage skip unit")
	}
	if s.SkipsByReason == nil || s.BlockingSkipsByReason == nil || s.AllowancesByReason == nil || s.UnacknowledgedSkipsByReason == nil || s.NonBudgetableGapsByCode == nil {
		return fmt.Errorf("coverage maps must be present objects")
	}
	for reason, count := range s.SkipsByReason {
		if (!blockingReason(reason) && reason != "ignored") || count < 0 {
			return fmt.Errorf("invalid coverage skip counts")
		}
	}
	for _, counts := range []map[string]int{s.BlockingSkipsByReason, s.AllowancesByReason, s.UnacknowledgedSkipsByReason} {
		for reason, count := range counts {
			if !blockingReason(reason) || count < 0 {
				return fmt.Errorf("invalid coverage blocking counts")
			}
		}
	}
	expectedStatus := "complete"
	for _, reason := range []string{"file_too_large", "binary_detected", "unreadable", "unknown"} {
		actual := s.SkipsByReason[reason]
		if s.BlockingSkipsByReason[reason] != actual {
			return fmt.Errorf("coverage actual and blocking maps disagree")
		}
		remaining := max(actual-s.AllowancesByReason[reason], 0)
		if s.UnacknowledgedSkipsByReason[reason] != remaining {
			return fmt.Errorf("coverage residual map disagrees with ceilings")
		}
		if actual > 0 && expectedStatus == "complete" {
			expectedStatus = "acknowledged"
		}
		if remaining > 0 {
			expectedStatus = "incomplete"
		}
	}
	for code, count := range s.NonBudgetableGapsByCode {
		switch code {
		case UnreadableDirectory, UnknownEntry, UnfollowedSymlink, UnrecognizedSkipReason, RefMissingLocalObjects, PrimaryBaselineUnscanned, NamesOnlyContentOmitted:
		default:
			return fmt.Errorf("invalid coverage gap code")
		}
		if count < 0 {
			return fmt.Errorf("invalid coverage gap count")
		}
		if count > 0 {
			expectedStatus = "incomplete"
		}
	}
	if s.Status != expectedStatus {
		return fmt.Errorf("coverage status disagrees with maps")
	}
	return nil
}
