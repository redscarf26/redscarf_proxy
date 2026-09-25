package modernworld

import "testing"

func TestSpellRankIdentities(t *testing.T) {
	for spell, entry := range spellRanks {
		first, ok := spellRanks[entry.first]
		if !ok || first.first != entry.first || first.rank != 1 {
			t.Fatalf("invalid chain for %d", spell)
		}
	}
	for _, tc := range []struct {
		requested, received uint32
		want                bool
	}{{1244, 1243, true}, {1461, 1459, true}, {49917, 45462, true}, {45462, 49917, false}, {1243, 1243, false}, {49917, 50463, false}, {47541, 47632, false}, {5143, 7268, false}, {999999, 0, false}} {
		if got := IsLowerSpellRank(tc.requested, tc.received); got != tc.want {
			t.Errorf("%d -> %d = %v", tc.requested, tc.received, got)
		}
	}
}
