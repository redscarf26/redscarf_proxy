package proxy

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"redscarf/internal/legacyauth"
	"redscarf/internal/modernworld"
)

func pendingForTest(spell uint32, counter uint64) modernworld.PendingCast {
	return modernworld.PendingCast{SpellID: spell, VisualID: modernworld.KnownSpellVisual(spell), ClientCastID: modernworld.GUID128{Low: counter}, ServerCastID: modernworld.ModernCastGUID(530, spell, 10000+counter)}
}

func TestPetLoadCooldownForwardsWithoutCompletingClientCast(t *testing.T) {
	for _, started := range []bool{false, true} {
		p := pendingForTest(46584, 1)
		p.Started = started
		previous := modernworld.ModernCastGUID(530, 46584, 999)
		s := &proxySession{currentCharacter: 7, currentMapID: 530, legacy: &legacyauth.Session{Username: "TEST"}, pendingCasts: []modernworld.PendingCast{p}, completedCastIDs: map[uint32]modernworld.GUID128{46584: previous}}
		server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		// Exact AzerothCore header-only packet: owner=7, spell=46584.
		cast, err := modernworld.ParseLegacySpellStartOrGo([]byte{1, 7, 1, 7, 0, 0xf8, 0xb5, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0}, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := server.forwardLegacySpell(s, cast, false); err != nil {
			t.Fatal(err)
		}
		if len(s.pendingInstance) != 1 || s.pendingInstance[0].Opcode != modernworld.SMSGSpellGo || len(s.pendingInstance[0].Body) <= 17 {
			t.Fatal("cooldown notification not translated to a complete modern GO, or sent PREPARE")
		}
		if !reflect.DeepEqual(s.pendingCasts, []modernworld.PendingCast{p}) || s.completedCastIDs[46584] != previous {
			t.Fatal("cooldown notification changed active/completed cast identity")
		}
		cast.PetLoadCooldown = false
		if err := server.forwardLegacySpell(s, cast, false); err != nil {
			t.Fatal(err)
		}
		if len(s.pendingCasts) != 0 || s.completedCastIDs[46584] != p.ServerCastID {
			t.Fatal("real same-spell GO could not complete after cooldown notification")
		}
	}
}

func TestUnrelatedCastCannotConsumePlayerRequest(t *testing.T) {
	// Includes the recorded 50463 theft, Death Coil's child 47632, item procs,
	// channel ticks and an unknown future proc. No blacklist is needed.
	for _, proc := range []uint32{50463, 47632, 51714, 7268, 999999} {
		for _, started := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/started=%v", proc, started), func(t *testing.T) {
				pending := pendingForTest(49917, 1)
				pending.Started = started
				s := &proxySession{currentCharacter: 7, pendingCasts: []modernworld.PendingCast{pending}}
				for _, start := range []bool{true, false} {
					cast := modernworld.SpellCastData{CasterGUID: 7, CasterUnit: 7, SpellID: proc}
					_, _, id, _, _, _, found := s.resolveSpellCastLocked(&cast, start)
					if found || id == pending.ServerCastID {
						t.Fatal("unrelated spell borrowed request identity")
					}
					if cast.VisualID != modernworld.KnownSpellVisual(proc) {
						t.Fatal("unrelated spell borrowed request visual")
					}
				}
				if !reflect.DeepEqual(s.pendingCasts, []modernworld.PendingCast{pending}) {
					t.Fatal("proc mutated pending request")
				}
				if !started {
					cast := modernworld.SpellCastData{CasterGUID: 7, CasterUnit: 7, SpellID: 49917}
					_, _, id, _, _, _, found := s.resolveSpellCastLocked(&cast, true)
					if !found || id != pending.ServerCastID {
						t.Fatal("real START lost CastID")
					}
				}
				cast := modernworld.SpellCastData{CasterGUID: 7, CasterUnit: 7, SpellID: 49917}
				_, _, id, _, _, _, found := s.resolveSpellCastLocked(&cast, false)
				if !found || id != pending.ServerCastID || len(s.pendingCasts) != 0 {
					t.Fatal("real GO lost CastID")
				}
			})
		}
	}
}

func TestUnrelatedFailureAndDamageDoNotBorrowPending(t *testing.T) {
	p := pendingForTest(49917, 1)
	p.Started = true
	s := &proxySession{currentCharacter: 7, pendingCasts: []modernworld.PendingCast{p}}
	for _, spell := range []uint32{50463, 47632, 999999} {
		if _, ok := s.matchPendingCastLocked(spell, false, true); ok {
			t.Fatal("common matcher crossed spell IDs")
		}
		if _, ok := s.matchPlayerCastFailureLocked(spell, true); ok {
			t.Fatal("failure consumed unrelated request")
		}
		id, _ := s.spellFailureCastLocked(spell, modernworld.ModernGUIDForLegacy(7, 0))
		if id == p.ServerCastID {
			t.Fatal("failure borrowed unrelated CastID")
		}
		fallback := modernworld.ModernCastGUID(530, spell, 123)
		if s.playerCombatLogCastIDLocked(spell, fallback) != fallback {
			t.Fatal("damage borrowed unrelated CastID")
		}
	}
	if len(s.pendingCasts) != 1 {
		t.Fatal("failure or log consumed pending")
	}
}

func TestDownrankBindsAtStartAndPreservesQueuedRequest(t *testing.T) {
	p := pendingForTest(1244, 1) // Power Word: Fortitude rank 2 -> rank 1
	p.TargetGUID = 9
	s := &proxySession{currentCharacter: 7, pendingCasts: []modernworld.PendingCast{p}}
	cast := modernworld.SpellCastData{CasterGUID: 7, CasterUnit: 7, SpellID: 1243, Target: modernworld.SpellCastTargets{Unit: modernworld.GUID128{Low: 9}}}
	_, _, id, _, _, _, found := s.resolveSpellCastLocked(&cast, true)
	if !found || id != p.ServerCastID || s.pendingCasts[0].ServerSpellID != 1243 {
		t.Fatal("verified downrank START did not bind")
	}
	queued := pendingForTest(1243, 2)
	s.pendingCasts = append(s.pendingCasts, queued)
	cast = modernworld.SpellCastData{CasterGUID: 7, CasterUnit: 7, SpellID: 1243}
	_, _, id, _, _, _, found = s.resolveSpellCastLocked(&cast, false)
	if !found || id != p.ServerCastID || len(s.pendingCasts) != 1 || s.pendingCasts[0] != queued {
		t.Fatal("downrank GO stole queued cast")
	}
	if s.completedCastIDs[1244] != id || s.completedCastIDs[1243] != id {
		t.Fatal("downrank completion aliases missing")
	}
}

func TestDownrankRequiresEvidence(t *testing.T) {
	for _, scenario := range []string{"wrong_target", "ambiguous", "go_without_start", "item", "pet", "up_rank", "unknown", "triggered"} {
		t.Run(scenario, func(t *testing.T) {
			p := pendingForTest(1244, 1)
			p.TargetGUID = 9
			s := &proxySession{currentCharacter: 7, pendingCasts: []modernworld.PendingCast{p}}
			cast := modernworld.SpellCastData{CasterGUID: 7, CasterUnit: 7, SpellID: 1243, Target: modernworld.SpellCastTargets{Unit: modernworld.GUID128{Low: 9}}}
			start := true
			switch scenario {
			case "wrong_target":
				cast.Target.Unit.Low = 10
			case "ambiguous":
				other := p
				other.SpellID = 1245
				s.pendingCasts = append(s.pendingCasts, other)
			case "go_without_start":
				start = false
			case "item":
				s.pendingCasts[0].CastItemGUID = 0x400000000000086b
			case "pet":
				s.pendingCasts[0].PetGUID = 10
			case "up_rank":
				s.pendingCasts[0].SpellID = 1243
				cast.SpellID = 1244
			case "unknown":
				cast.SpellID = 999999
			case "triggered":
				cast.CastFlags = 1
			}
			before := append([]modernworld.PendingCast(nil), s.pendingCasts...)
			_, _, _, _, _, _, found := s.resolveSpellCastLocked(&cast, start)
			if found || !reflect.DeepEqual(s.pendingCasts, before) {
				t.Fatal("unproven rank alias consumed/mutated request")
			}
		})
	}
}

func TestItemReagentCastMayCompleteAsPlayer(t *testing.T) {
	const item = uint64(0x400000000000086b)
	p := pendingForTest(123, 1)
	p.CastItemGUID = item
	s := &proxySession{currentCharacter: 7, pendingCasts: []modernworld.PendingCast{p}}
	start := modernworld.SpellCastData{CasterGUID: item, CasterUnit: 7, SpellID: 123}
	s.resolveSpellCastLocked(&start, true)
	goCast := modernworld.SpellCastData{CasterGUID: 7, CasterUnit: 7, SpellID: 123}
	_, _, id, _, _, _, found := s.resolveSpellCastLocked(&goCast, false)
	if !found || id != p.ServerCastID || len(s.pendingCasts) != 0 {
		t.Fatal("consumed reagent item lost its completion")
	}
}

func TestRecordedPlagueStrikeSequenceSendsPrepareOnlyForRealCast(t *testing.T) {
	p := pendingForTest(49917, 1)
	s := &proxySession{currentCharacter: 7, currentMapID: 530, legacy: &legacyauth.Session{Username: "TEST"}, pendingCasts: []modernworld.PendingCast{p}}
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	// Replay the 19:36:49 ordering through the actual outgoing packet path.
	for _, event := range []struct {
		spell uint32
		start bool
	}{{50463, false}, {49917, true}, {49917, false}} {
		cast := modernworld.SpellCastData{CasterGUID: 7, CasterUnit: 7, SpellID: event.spell}
		if err := server.forwardLegacySpell(s, cast, event.start); err != nil {
			t.Fatal(err)
		}
	}
	want := []uint16{modernworld.SMSGSpellGo, modernworld.SMSGSpellPrepare, modernworld.SMSGSpellStart, modernworld.SMSGSpellGo}
	if len(s.pendingInstance) != len(want) {
		t.Fatalf("packets=%d want=%d (unexpected proc PREPARE?)", len(s.pendingInstance), len(want))
	}
	for i, opcode := range want {
		if s.pendingInstance[i].Opcode != opcode {
			t.Fatalf("packet %d opcode=%d", i, s.pendingInstance[i].Opcode)
		}
	}
	if !bytes.Equal(s.pendingInstance[1].Body, modernworld.EncodeSpellPrepare(p.ClientCastID, p.ServerCastID)) {
		t.Fatal("PREPARE did not preserve client/server binding")
	}
	if s.completedCastIDs[50463] == p.ServerCastID || s.completedCastIDs[49917] != p.ServerCastID || len(s.pendingCasts) != 0 {
		t.Fatal("completed casts have incorrect identity")
	}
}

func TestDownrankFailureBeforeAndAfterStart(t *testing.T) {
	for _, started := range []bool{false, true} {
		p := pendingForTest(1244, 1)
		p.Started = started
		if started {
			p.ServerSpellID = 1243
		}
		s := &proxySession{currentCharacter: 7, pendingCasts: []modernworld.PendingCast{p}}
		id, _ := s.spellFailureCastLocked(1243, modernworld.ModernGUIDForLegacy(7, 0))
		if id != p.ServerCastID || len(s.pendingCasts) != 1 {
			t.Fatal("failure notification lost downrank identity or consumed it")
		}
		got, ok := s.matchPlayerCastFailureLocked(1243, true)
		if !ok || got.ServerCastID != id || len(s.pendingCasts) != 0 {
			t.Fatal("CAST_FAILED did not terminate downrank request")
		}
	}
}

func TestRepeatedDownrankRequestsKeepFIFO(t *testing.T) {
	first := pendingForTest(1244, 1)
	first.TargetGUID = 9
	second := pendingForTest(1244, 2)
	second.TargetGUID = 9
	s := &proxySession{currentCharacter: 7, pendingCasts: []modernworld.PendingCast{first, second}}
	for _, pending := range []modernworld.PendingCast{first, second} {
		for _, start := range []bool{true, false} {
			cast := modernworld.SpellCastData{CasterGUID: 7, CasterUnit: 7, SpellID: 1243, Target: modernworld.SpellCastTargets{Unit: modernworld.GUID128{Low: 9}}}
			_, _, id, _, _, _, found := s.resolveSpellCastLocked(&cast, start)
			if !found || id != pending.ServerCastID {
				t.Fatal("repeated downrank requests lost FIFO identity")
			}
		}
	}
	if len(s.pendingCasts) != 0 {
		t.Fatal("completed downrank requests remained queued")
	}
}

func TestRankRelationshipAloneCannotAssociateDamageOrGo(t *testing.T) {
	p := pendingForTest(1244, 1)
	p.TargetGUID = 9
	s := &proxySession{currentCharacter: 7, pendingCasts: []modernworld.PendingCast{p}}
	if _, found := s.matchPendingCastLocked(1243, false, true); found {
		t.Fatal("public matcher inferred a rank alias")
	}
	fallback := modernworld.ModernCastGUID(530, 1243, 987)
	if s.playerCombatLogCastIDLocked(1243, fallback) != fallback {
		t.Fatal("damage log inferred a rank alias")
	}
	if len(s.pendingCasts) != 1 {
		t.Fatal("unbound rank event consumed pending")
	}
}

func TestCastOwnershipSeparatesPlayerPetAndItems(t *testing.T) {
	const pet = uint64(0xf14000000800001e)
	const item = uint64(0x400000000000086b)
	playerRequest := pendingForTest(123, 1)
	petRequest := pendingForTest(123, 2)
	petRequest.PetGUID = pet
	itemRequest := pendingForTest(123, 3)
	itemRequest.CastItemGUID = item
	s := &proxySession{currentCharacter: 7, pendingCasts: []modernworld.PendingCast{playerRequest, petRequest, itemRequest}}
	for _, tc := range []struct {
		caster, unit uint64
		want         modernworld.PendingCast
	}{{pet, pet, petRequest}, {item, 7, itemRequest}, {7, 7, playerRequest}} {
		cast := modernworld.SpellCastData{CasterGUID: tc.caster, CasterUnit: tc.unit, SpellID: 123}
		_, _, id, _, _, _, found := s.resolveSpellCastLocked(&cast, false)
		if !found || id != tc.want.ServerCastID {
			t.Fatal("completion crossed cast ownership")
		}
	}
	s.pendingCasts = []modernworld.PendingCast{playerRequest, petRequest}
	got, ok := s.matchPetCastFailureLocked(123)
	if !ok || got.ServerCastID != petRequest.ServerCastID || len(s.pendingCasts) != 1 || s.pendingCasts[0] != playerRequest {
		t.Fatal("pet failure stole player request")
	}
}

func TestResolveSpellCastTransportKeepsMOTransportHigh(t *testing.T) {
	const ship = uint64(0x1fc0000000000015)
	session := &proxySession{
		objectGUIDs: map[uint64]modernworld.GUID128{
			ship: modernworld.ModernGUIDForLegacy(ship, 631),
		},
	}
	modern := session.objectGUIDs[ship]
	if modern.Low != 0 || modern.High == 0 {
		t.Fatalf("MO_TRANSPORT modern GUID=%+v, want Low=0 High!=0", modern)
	}
	loc := &modernworld.SpellTargetLocation{Transport: modern, Location: modernworld.Vec3{X: 1, Y: 2, Z: 3}}
	if got := resolveSpellCastTransportLocked(session, loc); got != ship {
		t.Fatalf("resolved dest transport=%#x, want %#x", got, ship)
	}
	if got := resolveSpellCastTransportLocked(session, &modernworld.SpellTargetLocation{Transport: modernworld.GUID128{Low: modern.Low}}); got != 0 {
		t.Fatalf("low-only lookup unexpectedly resolved %#x", got)
	}
}

func TestRemapSpellTargetTransportKeepsMOTransportHigh(t *testing.T) {
	const ship = uint64(0x1fc0000000000017)
	session := &proxySession{
		currentMapID: 631,
		objectGUIDs: map[uint64]modernworld.GUID128{
			ship: modernworld.ModernGUIDForLegacy(ship, 631),
		},
	}
	want := session.objectGUIDs[ship]
	loc := &modernworld.SpellTargetLocation{Transport: modernworld.GUID128{Low: ship}, Location: modernworld.Vec3{X: 6, Y: 1, Z: 20}}
	remapSpellTargetTransportLocked(session, loc)
	if loc.Transport != want {
		t.Fatalf("remapped dest transport=%+v, want %+v", loc.Transport, want)
	}
	already := *loc
	remapSpellTargetTransportLocked(session, &already)
	if already.Transport != want {
		t.Fatalf("second remap changed transport to %+v", already.Transport)
	}
}
