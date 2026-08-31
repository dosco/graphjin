package eval

import "testing"

// Every run recorded before sampling was configurable must keep the identity
// it had, or the published board stops comparing with itself.
//
// The golden below was captured from the binary as it stood before top-p and
// the split fields existed. It holds because those fields are omitted at their
// zero values: the identity is a hash of marshalled JSON, so a field that
// marshals to nothing changes nothing. Any future provenance field must be
// added the same way, and this test is where getting that wrong is caught.
func TestSuiteIdentityUnchangedForRunsThatPinNoSampling(t *testing.T) {
	const golden = "ee58275f2d901a1570f6633c84942e74"
	report := Report{
		Mode: RunModeBenchmark, SuiteFingerprint: "suite-abc",
		DatasetFingerprint: DatasetFingerprint{CatalogHash: "cat-1", SeedManifestHash: "seed-1"},
		Provenance:         RunProvenance{Seed: 23, Repeats: 3, MaxSteps: 8},
		RewardVersion:      RewardVersion,
	}
	if got := SuiteIdentity(report); got != golden {
		t.Fatalf("the identity of an existing run moved: got %s, want %s", got, golden)
	}
	// And a run that did pin sampling is a different thing, which is the point
	// of recording it at all.
	pinned := report
	pinned.Provenance.Temperature = 0.8
	if SuiteIdentity(pinned) == golden {
		t.Fatal("a run sampled at a different temperature must not claim the same identity")
	}
	withTopP := report
	withTopP.Provenance.TopP = 0.9
	if SuiteIdentity(withTopP) == golden {
		t.Fatal("a run that pinned top-p must not claim the same identity")
	}
	// Nothing is reported as drifting between two runs that pinned nothing.
	if drift := SuiteIdentityMismatches(report, report); len(drift) != 0 {
		t.Fatalf("a run must not drift from itself: %v", drift)
	}
	if drift := SuiteIdentityMismatches(report, withTopP); len(drift) != 1 || drift[0] != "provenance.top_p" {
		t.Fatalf("top-p drift must be named precisely: %v", drift)
	}
}
