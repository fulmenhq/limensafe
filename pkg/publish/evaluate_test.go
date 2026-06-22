package publish

import (
	"reflect"
	"testing"

	"github.com/fulmenhq/limensafe/pkg/extractor"
)

func branch(name string) extractor.RemoteRef {
	return extractor.RemoteRef{Name: name, FullName: "refs/heads/" + name, Kind: extractor.RefKindBranch, LocalObjects: true}
}
func tag(name string) extractor.RemoteRef {
	return extractor.RemoteRef{Name: name, FullName: "refs/tags/" + name, Kind: extractor.RefKindTag, LocalObjects: true}
}

func findVerdict(v SurfaceVerdict, name string) *RefVerdict {
	for i := range v.Refs {
		if v.Refs[i].Ref.Name == name {
			return &v.Refs[i]
		}
	}
	return nil
}

// The headline case: main is clean of an entity that a backup branch carries.
// The backup branch is flagged diverges_from_primary, publish_safe is false.
func TestEvaluate_DivergentBackupBranchFlagged(t *testing.T) {
	in := EvaluateInput{
		Refs:    []extractor.RemoteRef{branch("main"), branch("backup/old")},
		Primary: "main",
		RefEntities: map[string][]string{
			"main":       {"e-codename-1"},
			"backup/old": {"e-codename-1", "e-client-1"}, // carries e-client-1 that main does not
		},
		RefScanned:             map[string]bool{"main": true, "backup/old": true},
		PrimaryBaselineScanned: true,
		DangerPatterns:         extractor.DefaultDangerPatterns,
		Remote:                 "origin",
	}
	v := Evaluate(in)
	if v.PublishSafe {
		t.Fatal("publish_safe must be false when a ref diverges")
	}
	bk := findVerdict(v, "backup/old")
	if bk == nil || !bk.LeakVector {
		t.Fatalf("backup/old should be a leak vector: %+v", bk)
	}
	// backup/old matches BOTH divergence and the backup/* name pattern.
	if !reflect.DeepEqual(bk.Reasons, []string{ReasonDivergesFromPrimary, ReasonNamePattern}) {
		t.Fatalf("reasons = %v, want [diverges_from_primary name_pattern]", bk.Reasons)
	}
	if !reflect.DeepEqual(bk.DivergentEntities, []string{"e-client-1"}) {
		t.Fatalf("divergent entities = %v, want [e-client-1]", bk.DivergentEntities)
	}
	if bk.SuggestedAction != "git push origin --delete backup/old" {
		t.Fatalf("suggested_action = %q", bk.SuggestedAction)
	}
	if !reflect.DeepEqual(v.LeakVectorRefs, []string{"backup/old"}) {
		t.Fatalf("leak_vector_refs = %v", v.LeakVectorRefs)
	}
	// main is the primary: never divergent, not flagged.
	if mn := findVerdict(v, "main"); mn == nil || mn.LeakVector {
		t.Fatalf("primary main must not be a leak vector: %+v", mn)
	}
}

// A name-pattern-only ref (no findings) is still flagged, even unscanned.
func TestEvaluate_NamePatternFlaggedWithoutScan(t *testing.T) {
	in := EvaluateInput{
		Refs:           []extractor.RemoteRef{branch("main"), branch("wip/x")},
		Primary:        "main",
		RefEntities:    map[string][]string{"main": {}},
		RefScanned:     map[string]bool{"main": true, "wip/x": false}, // wip/x not scanned
		DangerPatterns: extractor.DefaultDangerPatterns,
	}
	v := Evaluate(in)
	wip := findVerdict(v, "wip/x")
	if wip == nil || !wip.LeakVector {
		t.Fatalf("wip/x should be name-pattern flagged: %+v", wip)
	}
	if !reflect.DeepEqual(wip.Reasons, []string{ReasonNamePattern}) {
		t.Fatalf("reasons = %v, want [name_pattern]", wip.Reasons)
	}
	if wip.Scanned {
		t.Fatal("wip/x should be marked not-scanned")
	}
	if wip.DivergentEntities != nil {
		t.Fatalf("unscanned ref should report no divergent entities, got %v", wip.DivergentEntities)
	}
	if v.PublishSafe {
		t.Fatal("publish_safe must be false with a name-pattern leak vector")
	}
}

// Every ref's entities are a subset of the primary's → safe to publish.
func TestEvaluate_AllCleanIsPublishSafe(t *testing.T) {
	in := EvaluateInput{
		Refs:    []extractor.RemoteRef{branch("main"), branch("feature/x"), tag("v0.1.0")},
		Primary: "main",
		RefEntities: map[string][]string{
			"main":      {"e-codename-1"},
			"feature/x": {"e-codename-1"}, // subset of primary
			"v0.1.0":    {},
		},
		RefScanned:             map[string]bool{"main": true, "feature/x": true, "v0.1.0": true},
		PrimaryBaselineScanned: true,
		DangerPatterns:         extractor.DefaultDangerPatterns,
	}
	v := Evaluate(in)
	if !v.PublishSafe {
		t.Fatalf("expected publish_safe, got verdict %+v", v)
	}
	if len(v.LeakVectorRefs) != 0 {
		t.Fatalf("no refs should be leak vectors, got %v", v.LeakVectorRefs)
	}
}

// A block-tier finding (even with no divergence/name match) blocks publish.
func TestEvaluate_BlockTierFindingBlocksPublish(t *testing.T) {
	in := EvaluateInput{
		Refs:                   []extractor.RemoteRef{branch("main")},
		Primary:                "main",
		RefEntities:            map[string][]string{"main": {"e-client-1"}},
		RefScanned:             map[string]bool{"main": true},
		PrimaryBaselineScanned: true,
		RefBlockTier:           map[string]bool{"main": true},
		DangerPatterns:         extractor.DefaultDangerPatterns,
	}
	v := Evaluate(in)
	if v.PublishSafe {
		t.Fatal("a block-tier finding on any ref must make publish_safe false")
	}
	// No leak-vector refs though — block-tier and leak_vector are distinct axes.
	if len(v.LeakVectorRefs) != 0 {
		t.Fatalf("block-tier alone is not a leak vector: %v", v.LeakVectorRefs)
	}
}

// When the primary baseline was not scanned (e.g. --tags-only with the primary
// branch outside the scanned surface), divergence must NOT be computed — an
// empty baseline would falsely flag every scanned entity. Only name patterns
// apply. (devrev CP2 P1 regression.)
func TestEvaluate_NoDivergenceWhenPrimaryBaselineUnscanned(t *testing.T) {
	in := EvaluateInput{
		Refs:    []extractor.RemoteRef{tag("v1.0.0")},
		Primary: "main", // not in the scanned surface
		RefEntities: map[string][]string{
			"v1.0.0": {"e-codename-1"}, // present, but also on the (unscanned) primary
		},
		RefScanned:             map[string]bool{"v1.0.0": true},
		PrimaryBaselineScanned: false, // primary not scanned
		DangerPatterns:         extractor.DefaultDangerPatterns,
	}
	v := Evaluate(in)
	tg := findVerdict(v, "v1.0.0")
	if tg == nil {
		t.Fatal("missing v1.0.0 verdict")
	}
	if tg.LeakVector {
		t.Fatalf("v1.0.0 must NOT be flagged divergent without a primary baseline: %+v", tg)
	}
	if !v.PublishSafe {
		t.Fatal("publish_safe should be true: no real divergence, no name match, no block tier")
	}
}

// A leak-vector tag gets the tags-qualified delete advice.
func TestEvaluate_TagSuggestedActionQualified(t *testing.T) {
	in := EvaluateInput{
		Refs:                   []extractor.RemoteRef{branch("main"), tag("v0.0.9-pre-rewrite")},
		Primary:                "main",
		RefEntities:            map[string][]string{"main": {}},
		RefScanned:             map[string]bool{"main": true, "v0.0.9-pre-rewrite": true},
		PrimaryBaselineScanned: true,
		DangerPatterns:         extractor.DefaultDangerPatterns,
		Remote:                 "upstream",
	}
	v := Evaluate(in)
	tg := findVerdict(v, "v0.0.9-pre-rewrite")
	if tg == nil || !tg.LeakVector {
		t.Fatalf("pre-rewrite tag should be flagged: %+v", tg)
	}
	if tg.SuggestedAction != "git push upstream --delete refs/tags/v0.0.9-pre-rewrite" {
		t.Fatalf("tag suggested_action = %q", tg.SuggestedAction)
	}
}
