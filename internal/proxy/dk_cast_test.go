package proxy

import (
	"testing"

	"redscarf/internal/modernworld"
)

func TestItemProcDoesNotConsumeQueuedPlagueStrike(t *testing.T) {
	const player = uint64(7)
	session := &proxySession{currentCharacter: player}
	for _, counter := range []uint64{101, 102} {
		session.pendingCasts = append(session.pendingCasts, modernworld.PendingCast{
			SpellID: 45462, VisualID: 342643,
			ServerCastID: modernworld.ModernCastGUID(530, 45462, counter),
		})
	}
	for _, counter := range []uint64{101, 102} {
		for _, start := range []bool{true, false} {
			cast := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: 45462}
			_, _, id, _, _, _, found := session.resolveSpellCastLocked(&cast, start)
			if !found || id != modernworld.ModernCastGUID(530, 45462, counter) {
				t.Fatalf("cast %d start=%v matched=%v id=%v", counter, start, found, id)
			}
		}
		proc := modernworld.SpellCastData{CasterGUID: 0x400000000000086b, CasterUnit: player, SpellID: 51714}
		_, _, _, _, _, _, found := session.resolveSpellCastLocked(&proc, false)
		if found {
			t.Fatal("item proc consumed a player action")
		}
	}
}

func TestRequestedItemSpellStillMatches(t *testing.T) {
	session := &proxySession{currentCharacter: 7, pendingCasts: []modernworld.PendingCast{{SpellID: 123, VisualID: 456, CastItemGUID: 0x400000000000086b}}}
	cast := modernworld.SpellCastData{CasterGUID: 0x400000000000086b, CasterUnit: 7, SpellID: 123}
	_, _, _, _, _, _, found := session.resolveSpellCastLocked(&cast, false)
	if !found || len(session.pendingCasts) != 0 {
		t.Fatal("requested item use did not complete")
	}
}
