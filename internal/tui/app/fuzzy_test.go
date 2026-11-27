package app

import "testing"

func TestFuzzyScoreOrdersExactPrefixWordSubstringSubsequence(t *testing.T) {
	order := []string{"dev", "devops", "team-dev", "pdevx", "d-e-v"}
	prev := 1 << 30
	for _, name := range order {
		s, ok := fuzzyScore(name, "dev")
		if !ok {
			t.Fatalf("%q does not match dev", name)
		}
		if s >= prev {
			t.Fatalf("%q scores %d, not below the previous %d", name, s, prev)
		}
		prev = s
	}
}

func TestFuzzyScoreIgnoresCaseAndRejectsNonMatches(t *testing.T) {
	if _, ok := fuzzyScore("General", "GEN"); !ok {
		t.Fatal("case matters")
	}
	if _, ok := fuzzyScore("random", "rx"); ok {
		t.Fatal("rx matches random")
	}
	if s, ok := fuzzyScore("anything", ""); !ok || s != 0 {
		t.Fatalf("empty query = %d, %v", s, ok)
	}
}

func TestFuzzyScorePrefersTighterSubsequences(t *testing.T) {
	tight, _ := fuzzyScore("gxnxral", "gnr")
	loose, _ := fuzzyScore("gxxxxxxnxxxxxxr", "gnr")
	if tight <= loose {
		t.Fatalf("tight %d <= loose %d", tight, loose)
	}
	if loose < 1 {
		t.Fatalf("a match scores %d", loose)
	}
}
