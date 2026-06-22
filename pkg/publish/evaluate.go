// Package publish implements the internal-brief publish-surface verdict: given the
// refs a repository would expose when made public and the protected entities
// found on each, it decides which refs are leak vectors and whether the repo
// is safe to publish.
//
// The verdict logic is deliberately pure (no git, no scanning engine) so it
// can be exhaustively unit-tested. The git enumeration and content scanning
// live in pkg/extractor and the audit-publish command, which feed their
// results into Evaluate.
package publish

import (
	"fmt"
	"sort"

	"github.com/fulmenhq/limensafe/pkg/extractor"
)

// Leak-vector reasons. A ref may carry both.
const (
	// ReasonDivergesFromPrimary: the ref carries protected entities the
	// primary ref does not — the high-signal backup-branch detector.
	ReasonDivergesFromPrimary = "diverges_from_primary"
	// ReasonNamePattern: the ref name matches a danger-name glob (backup/*,
	// *pre-rewrite*, …). Flagged even when content was not scanned.
	ReasonNamePattern = "name_pattern"
)

// EvaluateInput is the fully-attributed input to the publish-surface verdict.
type EvaluateInput struct {
	// Refs is the enumerated publish surface (branches + tags).
	Refs []extractor.RemoteRef
	// Primary is the short name of the ref divergence is measured against.
	Primary string
	// RefEntities maps a ref name to the protected entity IDs found on it
	// (deduped). Absent/empty for refs whose content was not scanned.
	RefEntities map[string][]string
	// PrimaryBaselineScanned reports whether the primary ref's content was
	// actually scanned to establish the divergence baseline. When false (e.g.
	// --names-only, or the primary's objects are not fetched locally),
	// divergence is NOT computed — an empty baseline would falsely flag every
	// scanned entity as divergent. Name-pattern flagging still applies.
	PrimaryBaselineScanned bool
	// RefScanned marks refs whose content was actually scanned (vs name-only,
	// e.g. an un-fetched remote ref).
	RefScanned map[string]bool
	// RefBlockTier marks refs carrying at least one block-tier finding.
	RefBlockTier map[string]bool
	// DangerPatterns is the danger-name glob list (defaults applied upstream).
	DangerPatterns []string
	// Remote names the publish remote, for suggested_action text.
	Remote string
}

// RefVerdict is the per-ref outcome.
type RefVerdict struct {
	Ref               extractor.RemoteRef
	LeakVector        bool
	Reasons           []string
	DivergentEntities []string
	SuggestedAction   string
	Scanned           bool
}

// SurfaceVerdict is the aggregated go/no-go report.
type SurfaceVerdict struct {
	PrimaryRef     string
	Refs           []RefVerdict
	LeakVectorRefs []string
	PublishSafe    bool
}

// Evaluate computes the publish-surface verdict. publish_safe is true iff no
// ref is a leak vector AND no ref carries a block-tier finding.
func Evaluate(in EvaluateInput) SurfaceVerdict {
	remote := in.Remote
	if remote == "" {
		remote = "origin"
	}
	primaryEntities := entitySet(in.RefEntities[in.Primary])

	verdicts := make([]RefVerdict, 0, len(in.Refs))
	var leakVectorRefs []string
	anyBlockTier := false

	for _, ref := range in.Refs {
		v := RefVerdict{Ref: ref, Scanned: in.RefScanned[ref.Name]}

		// Divergence: entities present on this ref but not on the primary.
		// The primary never diverges from itself. Only computed when the
		// primary baseline was actually scanned — otherwise an empty baseline
		// would flag every scanned entity as divergent (e.g. --tags-only with
		// the primary branch outside the scanned surface).
		if in.PrimaryBaselineScanned && ref.Name != in.Primary {
			var divergent []string
			for _, ent := range in.RefEntities[ref.Name] {
				if !primaryEntities[ent] {
					divergent = append(divergent, ent)
				}
			}
			if len(divergent) > 0 {
				sort.Strings(divergent)
				v.DivergentEntities = divergent
				v.Reasons = append(v.Reasons, ReasonDivergesFromPrimary)
			}
		}

		// Name-pattern flag — independent of content, so it fires even for
		// un-fetched (unscanned) refs.
		if _, ok := extractor.MatchesDangerPattern(ref.Name, in.DangerPatterns); ok {
			v.Reasons = append(v.Reasons, ReasonNamePattern)
		}

		v.LeakVector = len(v.Reasons) > 0
		if v.LeakVector {
			v.SuggestedAction = suggestedAction(remote, ref)
			leakVectorRefs = append(leakVectorRefs, ref.Name)
		}
		if in.RefBlockTier[ref.Name] {
			anyBlockTier = true
		}
		verdicts = append(verdicts, v)
	}

	sort.Strings(leakVectorRefs)
	return SurfaceVerdict{
		PrimaryRef:     in.Primary,
		Refs:           verdicts,
		LeakVectorRefs: leakVectorRefs,
		PublishSafe:    len(leakVectorRefs) == 0 && !anyBlockTier,
	}
}

// suggestedAction returns the text advice for removing a leak-vector ref.
// Detection + advice only — limensafe never runs this.
func suggestedAction(remote string, ref extractor.RemoteRef) string {
	if ref.Kind == extractor.RefKindTag {
		return fmt.Sprintf("git push %s --delete refs/tags/%s", remote, ref.Name)
	}
	return fmt.Sprintf("git push %s --delete %s", remote, ref.Name)
}

func entitySet(entities []string) map[string]bool {
	set := make(map[string]bool, len(entities))
	for _, e := range entities {
		set[e] = true
	}
	return set
}
