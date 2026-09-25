package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"redscarf/internal/bnet"
	"redscarf/internal/legacyauth"
	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
	"redscarf/internal/realm"
)

func TestAddonWhisperPlayerNotFoundSuppression(t *testing.T) {
	now := time.Unix(100, 0)
	session := &proxySession{}
	session.rememberAddonWhisperTargetLocked("S1alliance", now)
	session.rememberAddonWhisperTargetLocked("S1alliance", now)
	if !session.consumeAddonWhisperFailureLocked("s1ALLIANCE", now.Add(time.Second)) {
		t.Fatal("recent addon whisper failure should be suppressed")
	}
	if !session.consumeAddonWhisperFailureLocked("S1alliance", now.Add(2*time.Second)) {
		t.Fatal("each outstanding addon whisper failure should be suppressed")
	}
	if session.consumeAddonWhisperFailureLocked("S1alliance", now.Add(3*time.Second)) {
		t.Fatal("ordinary later player-not-found should not be suppressed")
	}
	session.rememberAddonWhisperTargetLocked("OldTarget", now)
	if session.consumeAddonWhisperFailureLocked("OldTarget", now.Add(16*time.Second)) {
		t.Fatal("expired addon whisper target should not suppress an error")
	}
}

func TestResolveCreatureSpellUsesKnownVisual(t *testing.T) {
	const creature = uint64(0xf13000024d002232)
	session := &proxySession{currentMapID: 0, objectGUIDs: map[uint64]modernworld.GUID128{
		creature: modernworld.ModernGUIDForLegacy(creature, 0),
	}}
	for _, test := range []struct {
		spellID uint32
		visual  uint32
	}{{20793, 244077}, {6304, 240259}, {32950, 316112}} {
		cast := modernworld.SpellCastData{CasterGUID: creature, CasterUnit: creature, SpellID: test.spellID}
		_, _, _, _, _, _, found := session.resolveSpellCastLocked(&cast, true)
		if found {
			t.Fatalf("creature spell %d unexpectedly matched a player pending cast", test.spellID)
		}
		if cast.VisualID != test.visual {
			t.Fatalf("creature spell %d visual=%d want=%d", test.spellID, cast.VisualID, test.visual)
		}
	}
}

func TestVisualIDDoesNotReuseUnrelatedStartedCast(t *testing.T) {
	session := &proxySession{pendingCasts: []modernworld.PendingCast{{SpellID: 1757, VisualID: 237428, Started: true}}}
	if got := session.exactVisualIDForSpellLocked(20793); got != 244077 {
		t.Fatalf("known Fireball visual=%d want=244077", got)
	}
	const creature = uint64(0xf13000024d002232)
	cast := modernworld.SpellCastData{CasterGUID: creature, CasterUnit: creature, SpellID: 999999}
	session.resolveSpellCastLocked(&cast, true)
	if got := cast.VisualID; got != 0 {
		t.Fatalf("unknown spell reused unrelated visual %d", got)
	}
}

func TestServerQueuesSpellUntilCasterObjectIsVisible(t *testing.T) {
	const player = uint64(0x42)
	const creature = uint64(0xf130000123000456)
	session := &proxySession{
		currentCharacter:   player,
		objectGUIDs:        map[uint64]modernworld.GUID128{player: modernworld.ModernGUIDForLegacy(player, 0)},
		visibleObjectGUIDs: map[uint64]struct{}{player: {}},
	}
	cast := modernworld.SpellCastData{CasterGUID: creature, CasterUnit: creature, SpellID: 836}
	if guid, wait := session.unknownSpellObjectLocked(cast); !wait || guid != creature {
		t.Fatalf("unknown caster wait=%v guid=%#x", wait, guid)
	}
	if !session.queueObjectSpellLocked(cast, false) || len(session.pendingObjectSpells) != 1 {
		t.Fatalf("spell was not queued: %#v", session.pendingObjectSpells)
	}
	if ready := session.takeReadyObjectSpellsLocked(); len(ready) != 0 {
		t.Fatalf("spell became ready before object create: %#v", ready)
	}
	session.objectGUIDs[creature] = modernworld.ModernGUIDForLegacy(creature, 0)
	session.markObjectVisibleLocked(creature)
	ready := session.takeReadyObjectSpellsLocked()
	if len(ready) != 1 || ready[0].cast.SpellID != 836 || len(session.pendingObjectSpells) != 0 {
		t.Fatalf("ready spell=%#v remaining=%#v", ready, session.pendingObjectSpells)
	}
}

func TestPlayerSpellDoesNotWaitForObjectVisibility(t *testing.T) {
	const player = uint64(0x42)
	session := &proxySession{currentCharacter: player}
	cast := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: 133}
	if guid, wait := session.unknownSpellObjectLocked(cast); wait || guid != 0 {
		t.Fatalf("player cast was queued: wait=%v guid=%#x", wait, guid)
	}
}

func TestUnmatchedLoginSpellUsesNormalCastGUID(t *testing.T) {
	const (
		player = uint64(3)
		mapID  = uint16(1)
	)
	modernPlayer := modernworld.ModernGUIDForLegacy(player, 0)
	session := &proxySession{
		currentCharacter: player,
		currentMapID:     mapID,
		objectGUIDs:      map[uint64]modernworld.GUID128{player: modernPlayer},
	}
	cast := modernworld.SpellCastData{
		CasterGUID: player,
		CasterUnit: player,
		SpellID:    836,
	}
	caster, unit, castID, _, _, _, found := session.resolveSpellCastLocked(&cast, false)
	if found {
		t.Fatal("server-originated login spell unexpectedly matched a client cast")
	}
	if caster != modernPlayer || unit != modernPlayer {
		t.Fatalf("caster=%#v unit=%#v want=%#v", caster, unit, modernPlayer)
	}
	wantCastID := modernworld.ModernCastGUID(mapID, cast.SpellID, player+uint64(cast.SpellID))
	if castID != wantCastID {
		t.Fatalf("unmatched login spell cast ID=%#v, want legacy proxy GUID %#v", castID, wantCastID)
	}
}

func TestTriggeredMissileSpellGosGetUniqueCastIDs(t *testing.T) {
	const (
		player  = uint64(0x42)
		spellID = uint32(7268)
	)
	modernPlayer := modernworld.ModernGUIDForLegacy(player, 0)
	session := &proxySession{
		currentCharacter: player,
		objectGUIDs:      map[uint64]modernworld.GUID128{player: modernPlayer},
	}
	first := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: spellID}
	_, _, firstID, _, _, _, found := session.resolveSpellCastLocked(&first, false)
	if found {
		t.Fatal("triggered missile unexpectedly matched a pending cast")
	}
	second := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: spellID}
	_, _, secondID, _, _, _, found := session.resolveSpellCastLocked(&second, false)
	if found {
		t.Fatal("second missile unexpectedly matched a pending cast")
	}
	if firstID == secondID {
		t.Fatalf("both missiles reused CastID %#v", firstID)
	}
	third := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: spellID}
	_, _, thirdID, _, _, _, _ := session.resolveSpellCastLocked(&third, false)
	if thirdID == firstID || thirdID == secondID {
		t.Fatalf("third missile reused a previous CastID %#v/%#v/%#v", firstID, secondID, thirdID)
	}
}

func TestCreatureTriggeredMissileSpellGosGetUniqueCastIDs(t *testing.T) {
	const (
		ooze    = uint64(0xf1300090230002c3)
		spellID = uint32(69832)
		mapID   = uint16(631)
	)
	modernOoze := modernworld.ModernGUIDForLegacy(ooze, mapID)
	session := &proxySession{
		currentMapID: mapID,
		objectGUIDs:  map[uint64]modernworld.GUID128{ooze: modernOoze},
	}
	first := modernworld.SpellCastData{CasterGUID: ooze, CasterUnit: ooze, SpellID: spellID}
	_, _, firstID, _, _, _, found := session.resolveSpellCastLocked(&first, false)
	if found {
		t.Fatal("creature missile unexpectedly matched a pending cast")
	}
	second := modernworld.SpellCastData{CasterGUID: ooze, CasterUnit: ooze, SpellID: spellID}
	_, _, secondID, _, _, _, found := session.resolveSpellCastLocked(&second, false)
	if found {
		t.Fatal("second creature missile unexpectedly matched a pending cast")
	}
	if firstID == secondID {
		t.Fatalf("both creature missiles reused CastID %#v", firstID)
	}
}

func TestCreatureSpellStartAndGoShareCastID(t *testing.T) {
	const (
		ooze    = uint64(0xf1300090230002c3)
		spellID = uint32(69839)
		mapID   = uint16(631)
	)
	modernOoze := modernworld.ModernGUIDForLegacy(ooze, mapID)
	session := &proxySession{
		currentMapID: mapID,
		objectGUIDs:  map[uint64]modernworld.GUID128{ooze: modernOoze},
	}
	start := modernworld.SpellCastData{CasterGUID: ooze, CasterUnit: ooze, SpellID: spellID}
	_, _, startID, _, _, _, _ := session.resolveSpellCastLocked(&start, true)
	goCast := modernworld.SpellCastData{CasterGUID: ooze, CasterUnit: ooze, SpellID: spellID}
	_, _, goID, _, _, _, _ := session.resolveSpellCastLocked(&goCast, false)
	if startID != goID {
		t.Fatalf("creature start/go CastID %#v vs %#v", startID, goID)
	}
}

func TestCreatureOozeExplosionFallsFromAbove(t *testing.T) {
	const (
		ooze    = uint64(0xf1300090230002c3)
		stalker = uint64(0xf1300094db0002e5)
		spellID = uint32(69832)
		mapID   = uint16(631)
	)
	session := &proxySession{
		currentMapID: mapID,
		objectGUIDs: map[uint64]modernworld.GUID128{
			ooze:    modernworld.ModernGUIDForLegacy(ooze, mapID),
			stalker: modernworld.ModernGUIDForLegacy(stalker, mapID),
		},
		objectTypes:     map[uint64]uint8{ooze: 3, stalker: 3},
		objectPositions: map[uint64][3]float32{ooze: {0, 0, 5}, stalker: {10, 20, 5}},
	}
	cast := modernworld.SpellCastData{
		CasterGUID: ooze,
		CasterUnit: ooze,
		SpellID:    spellID,
		Target: modernworld.SpellCastTargets{
			Flags: 0x2,
			Unit:  modernworld.GUID128{Low: stalker},
		},
	}
	session.resolveSpellCastLocked(&cast, false)
	if cast.Target.Dst == nil || cast.Target.Dst.Location != (modernworld.Vec3{X: 10, Y: 20, Z: 5}) {
		t.Fatalf("dest %#v", cast.Target.Dst)
	}
	if cast.Target.Src == nil || cast.Target.Src.Location != (modernworld.Vec3{X: 10, Y: 20, Z: 35}) {
		t.Fatalf("src %#v", cast.Target.Src)
	}
	if cast.TravelTime != 3750 {
		t.Fatalf("travel %d want 3750", cast.TravelTime)
	}
}

func TestSindragosaFrostBombFallsFromAbove(t *testing.T) {
	const (
		dragon  = uint64(0xf130008ff50004af)
		spellID = uint32(69846)
		mapID   = uint16(631)
	)
	dest := modernworld.Vec3{X: 4415, Y: 2484, Z: 203}
	session := &proxySession{
		currentMapID:    mapID,
		objectGUIDs:     map[uint64]modernworld.GUID128{dragon: modernworld.ModernGUIDForLegacy(dragon, mapID)},
		objectTypes:     map[uint64]uint8{dragon: 3},
		objectPositions: map[uint64][3]float32{dragon: {4476, 2484, 248}},
	}
	cast := modernworld.SpellCastData{
		CasterGUID: dragon,
		CasterUnit: dragon,
		SpellID:    spellID,
		Target: modernworld.SpellCastTargets{
			Flags: 0x40,
			Dst:   &modernworld.SpellTargetLocation{Location: dest},
		},
	}
	session.resolveSpellCastLocked(&cast, false)
	if cast.Target.Dst == nil || cast.Target.Dst.Location != dest {
		t.Fatalf("dest %#v", cast.Target.Dst)
	}
	if cast.Target.Src == nil || cast.Target.Src.Location != (modernworld.Vec3{X: 4415, Y: 2484, Z: 248}) {
		t.Fatalf("src %#v", cast.Target.Src)
	}
	if cast.TravelTime != 5625 {
		t.Fatalf("travel %d want 5625", cast.TravelTime)
	}
}

func TestPutricideSlimePuddleDoesNotGetFallingTrajectory(t *testing.T) {
	const (
		puddle  = uint64(0xf13000933a0004b6)
		spellID = uint32(70346)
		mapID   = uint16(631)
	)
	dest := modernworld.Vec3{X: 10, Y: 20, Z: 5}
	session := &proxySession{
		currentMapID:    mapID,
		objectGUIDs:     map[uint64]modernworld.GUID128{puddle: modernworld.ModernGUIDForLegacy(puddle, mapID)},
		objectTypes:     map[uint64]uint8{puddle: 3},
		objectPositions: map[uint64][3]float32{puddle: {10, 20, 5}},
	}
	cast := modernworld.SpellCastData{
		CasterGUID: puddle,
		CasterUnit: puddle,
		SpellID:    spellID,
		Target: modernworld.SpellCastTargets{
			Flags: 0x40,
			Dst:   &modernworld.SpellTargetLocation{Location: dest},
		},
	}
	session.resolveSpellCastLocked(&cast, false)
	if cast.TravelTime != 0 {
		t.Fatalf("slime puddle travel %d, want 0", cast.TravelTime)
	}
	if cast.Target.Src != nil {
		t.Fatalf("slime puddle src %#v", cast.Target.Src)
	}
}

func TestUnmatchedDestSpellGoDoesNotFallFromAbove(t *testing.T) {
	const (
		caster  = uint64(0xf1300093280004b8)
		spellID = uint32(70542)
		mapID   = uint16(631)
	)
	session := &proxySession{
		currentMapID:    mapID,
		objectGUIDs:     map[uint64]modernworld.GUID128{caster: modernworld.ModernGUIDForLegacy(caster, mapID)},
		objectTypes:     map[uint64]uint8{caster: 3},
		objectPositions: map[uint64][3]float32{caster: {0, 0, 5}},
	}
	cast := modernworld.SpellCastData{
		CasterGUID: caster,
		CasterUnit: caster,
		SpellID:    spellID,
		Target: modernworld.SpellCastTargets{
			Flags: 0x40,
			Dst:   &modernworld.SpellTargetLocation{Location: modernworld.Vec3{X: 20, Y: 0, Z: 5}},
		},
	}
	session.resolveSpellCastLocked(&cast, false)
	if cast.TravelTime != 0 || cast.Target.Src != nil {
		t.Fatalf("non-ooze dest go travel=%d src=%#v", cast.TravelTime, cast.Target.Src)
	}
}

func TestChanneledTriggerDoesNotStealPendingCast(t *testing.T) {
	const player = uint64(0x42)
	modernPlayer := modernworld.ModernGUIDForLegacy(player, 0)
	channel := modernworld.PendingCast{
		SpellID:      5143,
		ClientCastID: modernworld.GUID128{Low: 9},
		ServerCastID: modernworld.ModernCastGUID(0, 5143, 10009),
		VisualID:     239917,
		Started:      true,
	}
	session := &proxySession{
		currentCharacter:      player,
		currentChanneledSpell: 5143,
		objectGUIDs:           map[uint64]modernworld.GUID128{player: modernPlayer},
		pendingCasts:          []modernworld.PendingCast{channel},
	}
	missile := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: 7268}
	_, _, _, _, _, _, found := session.resolveSpellCastLocked(&missile, false)
	if found {
		t.Fatal("arcane missile tick stole the channeled pending cast")
	}
	if len(session.pendingCasts) != 1 || session.pendingCasts[0].ServerCastID != channel.ServerCastID {
		t.Fatalf("pending after missile tick: %#v", session.pendingCasts)
	}
}

func TestSpellGoConsumesPendingCastBeforeRepeatedUse(t *testing.T) {
	const player = uint64(0x42)
	modernPlayer := modernworld.ModernGUIDForLegacy(player, 0)
	session := &proxySession{
		currentCharacter: player,
		objectGUIDs:      map[uint64]modernworld.GUID128{player: modernPlayer},
	}

	for index, counter := range []uint64{101, 202} {
		pending := modernworld.PendingCast{
			SpellID:      133,
			ClientCastID: modernworld.GUID128{Low: counter},
			ServerCastID: modernworld.ModernCastGUID(0, 133, counter+10000),
			VisualID:     236677,
		}
		session.pendingCasts = append(session.pendingCasts, pending)

		start := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: 133}
		_, _, startID, _, _, started, found := session.resolveSpellCastLocked(&start, true)
		if !found || startID != pending.ServerCastID || !started.Started {
			t.Fatalf("cast %d start matched=%v id=%#v pending=%#v", index, found, startID, started)
		}

		goCast := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: 133}
		_, _, goID, _, _, completed, found := session.resolveSpellCastLocked(&goCast, false)
		if !found || goID != pending.ServerCastID || completed.ServerCastID != pending.ServerCastID {
			t.Fatalf("cast %d go matched=%v id=%#v pending=%#v", index, found, goID, completed)
		}
		if len(session.pendingCasts) != 0 {
			t.Fatalf("cast %d left %d pending entries after SPELL_GO", index, len(session.pendingCasts))
		}
	}
}

func TestSpellFailureCastReusesPendingServerID(t *testing.T) {
	const (
		player  = uint64(0x42)
		spellID = uint32(133)
	)
	modernPlayer := modernworld.ModernGUIDForLegacy(player, 0)
	pending := modernworld.PendingCast{
		SpellID:      spellID,
		ClientCastID: modernworld.GUID128{Low: 7},
		ServerCastID: modernworld.ModernCastGUID(0, spellID, 10007),
		VisualID:     236677,
		Started:      true,
	}
	session := &proxySession{
		currentCharacter: player,
		objectGUIDs:      map[uint64]modernworld.GUID128{player: modernPlayer},
		pendingCasts:     []modernworld.PendingCast{pending},
	}
	castID, visual := session.spellFailureCastLocked(spellID, modernPlayer)
	if castID != pending.ServerCastID || visual != pending.VisualID {
		t.Fatalf("failure cast=%#v visual=%d want %#v/%d", castID, visual, pending.ServerCastID, pending.VisualID)
	}
	if len(session.pendingCasts) != 1 {
		t.Fatal("spell-failed-other must not consume the pending cast before CAST_FAILED")
	}
}

func TestCreatureSpellFailureDoesNotStealPendingInterrupt(t *testing.T) {
	const (
		player     = uint64(0x7)
		creature   = uint64(0xf130004cf5000ca8)
		mapID      = uint16(530)
		fireball   = uint32(9053)
		mindFreeze = uint32(47528)
	)
	modernPlayer := modernworld.ModernGUIDForLegacy(player, mapID)
	modernCreature := modernworld.ModernGUIDForLegacy(creature, mapID)
	pending := modernworld.PendingCast{
		SpellID:      mindFreeze,
		ClientCastID: modernworld.GUID128{Low: 7},
		ServerCastID: modernworld.ModernCastGUID(mapID, mindFreeze, 10007),
		VisualID:     353918,
		Started:      true,
	}
	playerFireball := modernworld.ModernCastGUID(mapID, fireball, 20007)
	session := &proxySession{
		currentCharacter: player,
		currentMapID:     mapID,
		objectGUIDs: map[uint64]modernworld.GUID128{
			player:   modernPlayer,
			creature: modernCreature,
		},
		pendingCasts:     []modernworld.PendingCast{pending},
		completedCastIDs: map[uint32]modernworld.GUID128{fireball: playerFireball},
		spellVisuals:     map[uint32]uint32{fireball: 241519, mindFreeze: 353918},
	}

	// AzerothCore emits the creature's SPELL_FAILED_OTHER between the
	// interrupt spell's START and GO, while Mind Freeze is still pending.
	castID, visual := session.spellFailureCastLocked(fireball, modernCreature)
	want := modernworld.ModernCastGUID(mapID, fireball, uint64(fireball)+modernCreature.Low)
	if castID != want || visual != 241519 {
		t.Fatalf("creature interrupt cast=%#v visual=%d want %#v/241519", castID, visual, want)
	}
	if castID == pending.ServerCastID || castID == playerFireball {
		t.Fatalf("creature interrupt reused a player CastID %#v", castID)
	}
	if len(session.pendingCasts) != 1 || session.pendingCasts[0].ServerCastID != pending.ServerCastID {
		t.Fatalf("creature interrupt consumed mind freeze pending: %#v", session.pendingCasts)
	}

	selfID, selfVisual := session.spellFailureCastLocked(mindFreeze, modernPlayer)
	if selfID != pending.ServerCastID || selfVisual != pending.VisualID {
		t.Fatalf("player failure cast=%#v visual=%d want %#v/%d", selfID, selfVisual, pending.ServerCastID, pending.VisualID)
	}
}

func TestSpellGoDoesNotConsumeQueuedRepeatOfSameSpell(t *testing.T) {
	const player = uint64(0x42)
	modernPlayer := modernworld.ModernGUIDForLegacy(player, 0)
	session := &proxySession{
		currentCharacter: player,
		objectGUIDs:      map[uint64]modernworld.GUID128{player: modernPlayer},
	}
	first := modernworld.PendingCast{
		SpellID:      403,
		ClientCastID: modernworld.GUID128{Low: 101},
		ServerCastID: modernworld.ModernCastGUID(0, 403, 10100),
		VisualID:     236603,
	}
	queued := modernworld.PendingCast{
		SpellID:      403,
		ClientCastID: modernworld.GUID128{Low: 202},
		ServerCastID: modernworld.ModernCastGUID(0, 403, 10200),
		VisualID:     236603,
	}
	session.pendingCasts = []modernworld.PendingCast{first, queued}

	start := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: 403}
	_, _, startID, _, _, _, found := session.resolveSpellCastLocked(&start, true)
	if !found || startID != first.ServerCastID {
		t.Fatalf("first start matched=%v id=%#v want=%#v", found, startID, first.ServerCastID)
	}

	goCast := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: 403}
	_, _, goID, _, _, _, found := session.resolveSpellCastLocked(&goCast, false)
	if !found || goID != first.ServerCastID {
		t.Fatalf("first go matched=%v id=%#v want=%#v", found, goID, first.ServerCastID)
	}
	if len(session.pendingCasts) != 1 || session.pendingCasts[0].ServerCastID != queued.ServerCastID || session.pendingCasts[0].Started {
		t.Fatalf("queued pending after first GO: %#v", session.pendingCasts)
	}

	start2 := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: 403}
	_, _, startID2, _, _, _, found := session.resolveSpellCastLocked(&start2, true)
	if !found || startID2 != queued.ServerCastID {
		t.Fatalf("queued start matched=%v id=%#v want=%#v", found, startID2, queued.ServerCastID)
	}
}

func TestDelayedCombatLogDoesNotStealNextLightningBoltCast(t *testing.T) {
	const player = uint64(0x42)
	modernPlayer := modernworld.ModernGUIDForLegacy(player, 0)
	session := &proxySession{
		currentCharacter: player,
		objectGUIDs:      map[uint64]modernworld.GUID128{player: modernPlayer},
	}
	first := modernworld.PendingCast{
		SpellID:      403,
		ClientCastID: modernworld.GUID128{Low: 1},
		ServerCastID: modernworld.ModernCastGUID(0, 403, 10001),
		VisualID:     236603,
	}
	next := modernworld.PendingCast{
		SpellID:      403,
		ClientCastID: modernworld.GUID128{Low: 2},
		ServerCastID: modernworld.ModernCastGUID(0, 403, 10002),
		VisualID:     236603,
	}
	session.pendingCasts = []modernworld.PendingCast{first}

	start := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: 403}
	session.resolveSpellCastLocked(&start, true)
	goCast := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: 403}
	_, _, firstGoID, _, _, _, found := session.resolveSpellCastLocked(&goCast, false)
	if !found || firstGoID != first.ServerCastID {
		t.Fatalf("first go matched=%v id=%#v", found, firstGoID)
	}

	session.pendingCasts = append(session.pendingCasts, next)
	start2 := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: 403}
	_, _, nextStartID, _, _, _, found := session.resolveSpellCastLocked(&start2, true)
	if !found || nextStartID != next.ServerCastID {
		t.Fatalf("next start matched=%v id=%#v", found, nextStartID)
	}

	fallback := modernworld.ModernCastGUID(0, 403, uint64(403)+modernPlayer.Low)
	logID := session.playerCombatLogCastIDLocked(403, fallback)
	if logID != first.ServerCastID {
		t.Fatalf("combat log cast ID=%#v want first GO %#v", logID, first.ServerCastID)
	}
	if len(session.pendingCasts) != 1 || !session.pendingCasts[0].Started || session.pendingCasts[0].ServerCastID != next.ServerCastID {
		t.Fatalf("combat log consumed the next pending: %#v", session.pendingCasts)
	}

	go2 := modernworld.SpellCastData{CasterGUID: player, CasterUnit: player, SpellID: 403}
	_, _, nextGoID, _, _, _, found := session.resolveSpellCastLocked(&go2, false)
	if !found || nextGoID != next.ServerCastID {
		t.Fatalf("next go matched=%v id=%#v want=%#v", found, nextGoID, next.ServerCastID)
	}
	if len(session.pendingCasts) != 0 {
		t.Fatalf("next GO left pending: %#v", session.pendingCasts)
	}
}

func TestCompletedQuestQueryForwardsToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}
	body := binary.LittleEndian.AppendUint32(nil, 2)
	body = binary.LittleEndian.AppendUint32(body, 33)
	body = binary.LittleEndian.AppendUint32(body, 76)
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGQueryQuestCompletionNPCs, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("completed-quest query was not handled")
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGQueryQuestsCompleted || !bytes.Equal(write.body, body) {
		t.Fatalf("unexpected completed-quest query: %#v", write)
	}
}

func TestPetNameQueryUsesPetNumberAndCoalescesDuplicates(t *testing.T) {
	const legacyPet = uint64(0xf140000003000002)
	modernPet := modernworld.ModernGUIDForLegacy(legacyPet, 571)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:       &legacyauth.Session{Username: "TEST"},
		legacyWorld:  legacyConnection,
		currentMapID: 571,
		objectGUIDs:  map[uint64]modernworld.GUID128{legacyPet: modernPet},
	}
	body := appendTestModernPackedGUID(nil, modernPet)
	for range 2 {
		handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGQueryPetName, Body: body})
		if err != nil || !handled {
			t.Fatalf("pet-name query handled=%v err=%v", handled, err)
		}
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGPetNameQuery || len(write.body) != 12 ||
		binary.LittleEndian.Uint32(write.body) != 3 || binary.LittleEndian.Uint64(write.body[4:]) != legacyPet {
		t.Fatalf("unexpected legacy pet-name query: %#v", write)
	}
	if len(legacyConnection.writes) != 0 {
		t.Fatalf("duplicate pet-name query was forwarded: %#v", <-legacyConnection.writes)
	}
	if pending := session.pendingPetNameGUIDs[3]; len(pending) != 1 || pending[0] != modernPet {
		t.Fatalf("pending pet-name GUIDs = %#v", pending)
	}
}

func TestGetMirrorImageDataForwardsLegacyUnpackedGUID(t *testing.T) {
	const legacyUnit = uint64(0xf130000001000042)
	modernUnit := modernworld.ModernGUIDForLegacy(legacyUnit, 631)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:       &legacyauth.Session{Username: "TEST"},
		legacyWorld:  legacyConnection,
		currentMapID: 631,
		objectGUIDs:  map[uint64]modernworld.GUID128{legacyUnit: modernUnit},
	}
	body := appendTestModernPackedGUID(nil, modernUnit)
	body = binary.LittleEndian.AppendUint32(body, 49)
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGGetMirrorImageData, Body: body})
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	want := modernworld.EncodeLegacyGetMirrorImageData(legacyUnit)
	if write.opcode != legacyworld.CMSGGetMirrorImageData || !bytes.Equal(write.body, want) {
		t.Fatalf("legacy write=%#v want opcode=%d body=%x", write, legacyworld.CMSGGetMirrorImageData, want)
	}
}

func TestRequestMirrorImageDataForwardsOnce(t *testing.T) {
	const legacyUnit = uint64(0xf130000001000042)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
	}
	if err := server.requestMirrorImageData(session, legacyUnit); err != nil {
		t.Fatal(err)
	}
	write := legacyConnection.nextWrite(t)
	want := modernworld.EncodeLegacyGetMirrorImageData(legacyUnit)
	if write.opcode != legacyworld.CMSGGetMirrorImageData || !bytes.Equal(write.body, want) {
		t.Fatalf("legacy write=%#v want opcode=%d body=%x", write, legacyworld.CMSGGetMirrorImageData, want)
	}
	if err := server.requestMirrorImageData(session, legacyUnit); err != nil {
		t.Fatal(err)
	}
	if len(legacyConnection.writes) != 0 {
		t.Fatalf("duplicate mirror-image query: %#v", <-legacyConnection.writes)
	}
}

func TestMirrorImageDataRelaysComponentedPacket(t *testing.T) {
	const legacyGUID = uint64(0xf130000001000042)
	unit := modernworld.ModernGUIDForLegacy(legacyGUID, 631)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:       &legacyauth.Session{Username: "TEST"},
		legacyWorld:  legacyConnection,
		currentMapID: 631,
		objectGUIDs:  map[uint64]modernworld.GUID128{legacyGUID: unit},
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	legacy := modernworld.EncodeLegacyUnpackedGUID(legacyGUID)
	legacy = binary.LittleEndian.AppendUint32(legacy, 50)
	legacy = append(legacy, 1, 1, 8, 2, 1, 11, 5, 2)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	for _, display := range []uint32{0, 0, 2163, 12647, 0, 9924, 9929, 0, 0, 0, 0} {
		legacy = binary.LittleEndian.AppendUint32(legacy, display)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGMirrorImageData, Body: legacy}

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("mirror-image data queued %d packets before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}
	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}

	wantOpcode, wantBody, err := modernworld.TranslateLegacyMirrorImageData(legacy, func(uint64) modernworld.GUID128 { return unit })
	if err != nil {
		t.Fatal(err)
	}
	session.worldMu.Lock()
	packet := session.pendingInstance[0]
	session.worldMu.Unlock()
	if packet.Opcode != wantOpcode || !bytes.Equal(packet.Body, wantBody) {
		t.Fatalf("mirror-image relay opcode=%d body=%x want opcode=%d body=%x", packet.Opcode, packet.Body, wantOpcode, wantBody)
	}
	if wantOpcode != modernworld.SMSGMirrorImageComponentedData {
		t.Fatalf("opcode = %d, want componented %d", wantOpcode, modernworld.SMSGMirrorImageComponentedData)
	}
}

func TestMirrorImageCreateRequestsLegacyAppearance(t *testing.T) {
	const legacyGUID = uint64(0xf130000001000042)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:       &legacyauth.Session{Username: "TEST"},
		legacyWorld:  legacyConnection,
		currentMapID: 631,
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	legacyConnection.reads <- legacyworld.Packet{
		Opcode: legacyworld.SMSGUpdateObject,
		Body:   testLegacyMirrorImageCreate(legacyGUID),
	}

	var foundMirror bool
	deadline := time.Now().Add(2 * time.Second)
	for !foundMirror {
		if time.Now().After(deadline) {
			t.Fatal("clone create did not query CMSG_GET_MIRROR_IMAGE_DATA")
		}
		select {
		case write := <-legacyConnection.writes:
			if write.opcode == legacyworld.CMSGGetMirrorImageData {
				want := modernworld.EncodeLegacyGetMirrorImageData(legacyGUID)
				if !bytes.Equal(write.body, want) {
					t.Fatalf("mirror-image query body=%x want=%x", write.body, want)
				}
				foundMirror = true
			}
		case <-time.After(20 * time.Millisecond):
		}
	}

	session.worldMu.Lock()
	_, queried := session.queriedMirrorImages[legacyGUID]
	session.worldMu.Unlock()
	if !queried {
		t.Fatal("clone GUID was not marked as queried")
	}

	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
}

func TestPetSetActionForwardsToLegacy(t *testing.T) {
	const legacyPet = uint64(0xf140000003000002)
	modernPet := modernworld.ModernGUIDForLegacy(legacyPet, 571)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:       &legacyauth.Session{Username: "TEST"},
		legacyWorld:  legacyConnection,
		currentMapID: 571,
		objectGUIDs:  map[uint64]modernworld.GUID128{legacyPet: modernPet},
	}

	// The 3.4.3 client expresses an auto-cast spell on the pet bar with the
	// 3.4.3 9-bit action type at bits 23..31. A spell action type of 0x101 maps
	// back to the legacy 0x81<<24 auto-cast state.
	const (
		modernAction = uint32(19685) | 0x101<<23
		legacyAction = uint32(19685) | 0x81<<24
	)
	body := appendTestModernPackedGUID(nil, modernPet)
	body = binary.LittleEndian.AppendUint32(body, 3) // pet action-bar slot
	body = binary.LittleEndian.AppendUint32(body, modernAction)

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGPetSetAction, Body: body})
	if err != nil || !handled {
		t.Fatalf("pet-set-action handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGPetSetAction {
		t.Fatalf("pet-set-action wrote opcode %#x, want legacy CMSG_PET_SET_ACTION", write.opcode)
	}
	if len(write.body) != 16 {
		t.Fatalf("legacy pet-set-action body has %d bytes, want 16", len(write.body))
	}
	if binary.LittleEndian.Uint64(write.body) != legacyPet {
		t.Fatalf("legacy pet-set-action pet=%#x", binary.LittleEndian.Uint64(write.body))
	}
	if got := binary.LittleEndian.Uint32(write.body[8:]); got != 3 {
		t.Fatalf("legacy pet-set-action slot=%d want 3", got)
	}
	if got := binary.LittleEndian.Uint32(write.body[12:]); got != legacyAction {
		t.Fatalf("legacy pet-set-action action=%#x want %#x", got, legacyAction)
	}

	if _, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGPetSetAction, Body: appendTestModernPackedGUID(nil, modernPet)}); err == nil {
		t.Fatal("pet-set-action with no entries must error")
	}
}

func TestPetRenameForwardsToLegacy(t *testing.T) {
	const legacyPet = uint64(0xf140000003000002)
	modernPet := modernworld.ModernGUIDForLegacy(legacyPet, 571)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:       &legacyauth.Session{Username: "TEST"},
		legacyWorld:  legacyConnection,
		currentMapID: 571,
		objectGUIDs:  map[uint64]modernworld.GUID128{legacyPet: modernPet},
	}

	body := appendTestModernPackedGUID(nil, modernPet)
	body = binary.LittleEndian.AppendUint32(body, 3)
	// 8-bit name length 4 ("Wolf") then declined=false, flushed to two bit bytes.
	body = append(body, 0x04, 0)
	body = append(body, "Wolf"...)

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGPetRename, Body: body})
	if err != nil || !handled {
		t.Fatalf("pet-rename handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGPetRename {
		t.Fatalf("pet-rename wrote opcode %#x, want legacy CMSG_PET_RENAME 0x177", write.opcode)
	}
	want := binary.LittleEndian.AppendUint64(nil, legacyPet)
	want = append(want, "Wolf"...)
	want = append(want, 0, 0)
	if !bytes.Equal(write.body, want) {
		t.Fatalf("legacy pet-rename body=%x want=%x", write.body, want)
	}

	uncached := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
	}
	handled, err = server.handleModernPlayPacket(uncached, nil, modernworld.Packet{Opcode: modernworld.CMSGPetRename, Body: body})
	if err != nil || !handled {
		t.Fatalf("uncached pet-rename handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGPetRename || binary.LittleEndian.Uint64(write.body) != legacyPet {
		t.Fatalf("uncached pet-rename opcode=%#x body=%x", write.opcode, write.body)
	}
}

func TestFactionReputationControlsForwardToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
	}

	// At-war on: the 3.4.3 checkbox sends a one-byte rep-list index.
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGSetFactionAtWar, Body: []byte{3}})
	if err != nil || !handled {
		t.Fatalf("at-war handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGSetFactionAtWar || !bytes.Equal(write.body, []byte{3, 0, 0, 0, 1}) {
		t.Fatalf("at-war write opcode=%#x body=%x", write.opcode, write.body)
	}

	// Not-at-war maps to the same 3.3.5a opcode with the flag cleared.
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGSetFactionNotAtWar, Body: []byte{3}})
	if err != nil || !handled {
		t.Fatalf("not-at-war handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGSetFactionAtWar || !bytes.Equal(write.body, []byte{3, 0, 0, 0, 0}) {
		t.Fatalf("not-at-war write opcode=%#x body=%x", write.opcode, write.body)
	}

	// Inactive toggle: 32-bit index plus a state bit.
	inactive := binary.LittleEndian.AppendUint32(nil, 9)
	inactive = append(inactive, 0x80)
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGSetFactionInactive, Body: inactive})
	if err != nil || !handled {
		t.Fatalf("inactive handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGSetFactionInactive || !bytes.Equal(write.body, []byte{9, 0, 0, 0, 1}) {
		t.Fatalf("inactive write opcode=%#x body=%x", write.opcode, write.body)
	}

	// Watched faction bar: 32-bit index only.
	watched := binary.LittleEndian.AppendUint32(nil, 9)
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGSetWatchedFaction, Body: watched})
	if err != nil || !handled {
		t.Fatalf("watched handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGSetWatchedFaction || !bytes.Equal(write.body, watched) {
		t.Fatalf("watched write opcode=%#x body=%x", write.opcode, write.body)
	}

	// A malformed at-war request must fail closed (dropped), never forwarded.
	if _, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGSetFactionAtWar, Body: nil}); err == nil {
		t.Fatal("empty at-war request must error")
	}
}

func TestSetFactionVisibleRelaysToClient(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	legacyConnection.reads <- legacyworld.Packet{
		Opcode: legacyworld.SMSGSetFactionVisible,
		Body:   binary.LittleEndian.AppendUint32(nil, 7),
	}

	// Poll until the relay has queued the modern packet; only then close so the
	// reader cannot race the packet with the closed signal.
	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("faction-visible queued %d packets before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}

	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
	session.worldMu.Lock()
	packet := session.pendingInstance[0]
	session.worldMu.Unlock()
	if packet.Opcode != modernworld.SMSGSetFactionVisible || !bytes.Equal(packet.Body, binary.LittleEndian.AppendUint32(nil, 7)) {
		t.Fatalf("faction-visible packet=%#v body=%x", packet.Opcode, packet.Body)
	}
}

func TestDuelResponseForwardsToLegacy(t *testing.T) {
	const legacyArbiter = uint64(0xf110000001000042)
	modernArbiter := modernworld.ModernGUIDForLegacy(legacyArbiter, 0)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyArbiter: modernArbiter},
	}

	for _, test := range []struct {
		name   string
		bits   byte
		opcode uint32
	}{
		{name: "accept", bits: 0x80, opcode: legacyworld.CMSGDuelAccepted},
		{name: "decline", bits: 0x00, opcode: legacyworld.CMSGDuelCancelled},
		{name: "forfeit", bits: 0x40, opcode: legacyworld.CMSGDuelCancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := appendTestModernPackedGUID(nil, modernArbiter)
			body = append(body, test.bits)
			handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGDuelResponse, Body: body})
			if err != nil || !handled {
				t.Fatalf("duel response handled=%v err=%v", handled, err)
			}
			write := legacyConnection.nextWrite(t)
			if write.opcode != test.opcode || len(write.body) != 8 || binary.LittleEndian.Uint64(write.body) != legacyArbiter {
				t.Fatalf("unexpected legacy duel response: %#v", write)
			}
		})
	}
}

func TestWhoAndPartyRequestsForwardToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	// Keep this player out of objectGUIDs to cover party actions for teammates
	// who are known by the group roster but are outside the local object cache.
	modernTarget := modernworld.ModernGUIDForLegacy(0x43, 0)
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
	}

	// No areas, filters or strings, followed by request ID 0x12345678.
	who := make([]byte, 1+20+5)
	who = binary.LittleEndian.AppendUint32(who, 0x12345678)
	who = append(who, 0) // WHO origin, added in build 54261
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGWho, Body: who})
	if err != nil || !handled {
		t.Fatalf("WHO handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGWho || len(write.body) != 26 {
		t.Fatalf("unexpected legacy WHO request: %#v", write)
	}
	if session.lastWhoRequestID != 0x12345678 {
		t.Fatalf("saved WHO request ID = %#x", session.lastWhoRequestID)
	}

	whisperTarget := "Alice"
	whisperText := "hello"
	whisper := binary.LittleEndian.AppendUint32(nil, 0)
	// 9-bit target length 5 followed by 11-bit text length 5, MSB first.
	whisper = append(whisper, 0x02, 0x80, 0x50)
	whisper = append(whisper, whisperTarget...)
	whisper = append(whisper, whisperText...)
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGChatMessageWhisper, Body: whisper})
	if err != nil || !handled {
		t.Fatalf("whisper handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGMessageChat || binary.LittleEndian.Uint32(write.body[:4]) != 7 || !bytes.Equal(write.body[8:], []byte("Alice\x00hello\x00")) {
		t.Fatalf("unexpected legacy whisper: %#v", write)
	}

	// PartyIndex=0, 9-bit name length=5, 9-bit realm length=0, empty GUID.
	invite := []byte{0, 0x02, 0x80, 0x00}
	invite = binary.LittleEndian.AppendUint32(invite, 0x01010001)
	invite = append(invite, 0, 0)
	invite = append(invite, "Alice"...)
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGPartyInvite, Body: invite})
	if err != nil || !handled {
		t.Fatalf("party invite handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGGroupInvite || !bytes.Equal(write.body, append([]byte("Alice\x00"), 0, 0, 0, 0)) {
		t.Fatalf("unexpected legacy party invite: %#v", write)
	}

	// HasPartyIndex=false, Accept=true, HasRolesDesired=false.
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGPartyInviteResponse, Body: []byte{0x40}})
	if err != nil || !handled {
		t.Fatalf("party accept handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGGroupAccept || len(write.body) != 4 {
		t.Fatalf("unexpected legacy party accept: %#v", write)
	}

	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGLeaveGroup, Body: []byte{0}})
	if err != nil || !handled {
		t.Fatalf("leave group handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGGroupDisband || len(write.body) != 0 {
		t.Fatalf("unexpected legacy leave-group request: %#v", write)
	}

	// Name length=5 (9 bits), note length=0 (10 bits), then the raw name.
	addFriend := append([]byte{0x02, 0x80, 0x00}, "Alice"...)
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGAddFriend, Body: addFriend})
	if err != nil || !handled {
		t.Fatalf("add friend handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGAddFriend || !bytes.Equal(write.body, []byte("Alice\x00\x00")) {
		t.Fatalf("unexpected legacy add-friend request: %#v", write)
	}

	if handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGRequestPartyJoinUpdates, Body: []byte{0, 1}}); err != nil || !handled {
		t.Fatalf("party join updates handled=%v err=%v", handled, err)
	}

	// PartyIndex=0 followed by the teammate's PackedGuid128.
	memberStats := appendTestModernPackedGUID([]byte{0}, modernTarget)
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGRequestPartyMemberStats, Body: memberStats})
	if err != nil || !handled {
		t.Fatalf("party-member stats handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGRequestPartyMemberStats || len(write.body) != 8 || binary.LittleEndian.Uint64(write.body) != 0x43 {
		t.Fatalf("unexpected legacy party-member request: %#v", write)
	}

	raidTarget := append(append([]byte(nil), memberStats...), 7)
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGUpdateRaidTarget, Body: raidTarget})
	if err != nil || !handled {
		t.Fatalf("raid target handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != uint32(legacyworld.MSGRaidTargetUpdate) || len(write.body) != 9 || write.body[0] != 7 || binary.LittleEndian.Uint64(write.body[1:]) != 0x43 {
		t.Fatalf("unexpected legacy raid-target request: %#v", write)
	}
}

func TestGroupManagementRequestsUse335Semantics(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	selfLegacy := uint64(0x42)
	targetLegacy := uint64(0x43)
	self := modernworld.ModernGUIDForLegacy(selfLegacy, 0)
	target := modernworld.ModernGUIDForLegacy(targetLegacy, 0)
	session := &proxySession{
		legacy:           &legacyauth.Session{Username: "TEST"},
		legacyWorld:      legacyConnection,
		currentCharacter: selfLegacy,
		partyLegacyGUID:  0x1001,
		partyGUID:        modernworld.GUID128{Low: 0x1001, High: uint64(33) << 58},
		partyMembers: map[uint64]modernworld.LegacyPartyMember{
			selfLegacy:   {GUID: selfLegacy, Name: "Alice", Roles: 2},
			targetLegacy: {GUID: targetLegacy, Name: "Bob", Roles: 4},
		},
		partyRoles: map[uint64]byte{selfLegacy: 2, targetLegacy: 4},
	}

	checkWrite := func(opcode uint32, body []byte) {
		t.Helper()
		write := legacyConnection.nextWrite(t)
		if write.opcode != opcode || !bytes.Equal(write.body, body) {
			t.Fatalf("legacy write opcode=%#x body=%x, want opcode=%#x body=%x", write.opcode, write.body, opcode, body)
		}
	}
	handle := func(opcode uint16, body []byte) {
		t.Helper()
		handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: opcode, Body: body})
		if err != nil || !handled {
			t.Fatalf("opcode %#x handled=%v err=%v body=%x", opcode, handled, err, body)
		}
	}

	handle(modernworld.CMSGDoReadyCheck, []byte{0})
	checkWrite(legacyworld.MSGRaidReadyCheck, nil)
	handle(modernworld.CMSGReadyCheckResponse, []byte{0x40}) // no party selector, ready=true
	checkWrite(legacyworld.MSGRaidReadyCheck, []byte{1})

	uninvite := []byte{0x01, 0x80} // hasParty=false, 8-bit reason length=3
	uninvite = appendTestModernPackedGUID(uninvite, target)
	uninvite = append(uninvite, "bye"...)
	handle(modernworld.CMSGPartyUninvite, uninvite)
	wantUninvite := binary.LittleEndian.AppendUint64(nil, targetLegacy)
	wantUninvite = append(wantUninvite, "bye\x00"...)
	checkWrite(legacyworld.CMSGGroupUninviteGUID, wantUninvite)

	leader := appendTestModernPackedGUID([]byte{0}, target) // no optional party selector
	handle(modernworld.CMSGSetPartyLeader, leader)
	checkWrite(legacyworld.CMSGGroupSetLeader, binary.LittleEndian.AppendUint64(nil, targetLegacy))

	// The client uses PartyIndex=-1 (0xff) when no party category is selected.
	// This still has to reach the legacy server instead of being mistaken for
	// an optional-field bit header.
	loot := appendTestModernPackedGUID([]byte{0xff, 2}, target)
	loot = binary.LittleEndian.AppendUint32(loot, 4)
	handle(modernworld.CMSGSetLootMethod, loot)
	wantLoot := binary.LittleEndian.AppendUint32(nil, 2)
	wantLoot = binary.LittleEndian.AppendUint64(wantLoot, targetLegacy)
	wantLoot = binary.LittleEndian.AppendUint32(wantLoot, 4)
	checkWrite(legacyworld.CMSGSetLootMethod, wantLoot)
	handle(modernworld.CMSGOptOutOfLoot, []byte{0x80})
	checkWrite(legacyworld.CMSGOptOutOfLoot, []byte{1, 0, 0, 0})
	handle(modernworld.CMSGOptOutOfLoot, []byte{0})
	checkWrite(legacyworld.CMSGOptOutOfLoot, []byte{0, 0, 0, 0})

	handle(modernworld.CMSGConvertRaid, []byte{0x80})
	checkWrite(legacyworld.CMSGGroupRaidConvert, nil)

	assistant := appendTestModernPackedGUID([]byte{0x40}, target) // no selector, apply=true
	handle(modernworld.CMSGSetAssistantLeader, assistant)
	wantAssistant := binary.LittleEndian.AppendUint64(nil, targetLegacy)
	wantAssistant = append(wantAssistant, 1)
	checkWrite(legacyworld.CMSGGroupAssistantLeader, wantAssistant)

	handle(modernworld.CMSGSetEveryoneIsAssistant, []byte{0x40})
	checkWrite(legacyworld.CMSGGroupAssistantLeader, wantAssistant)

	assignment := append([]byte{0x40, 1}, appendTestModernPackedGUID(nil, target)...)
	handle(modernworld.CMSGSetPartyAssignment, assignment)
	wantAssignment := []byte{1, 1}
	wantAssignment = binary.LittleEndian.AppendUint64(wantAssignment, targetLegacy)
	checkWrite(legacyworld.MSGPartyAssignment, wantAssignment)

	handle(modernworld.CMSGInitiateRolePoll, []byte{0})
	setRole := appendTestModernPackedGUID([]byte{0}, self)
	setRole = append(setRole, 8)
	handle(modernworld.CMSGSetRole, setRole)
	checkWrite(legacyworld.CMSGDFSetRoles, []byte{8})
	handle(modernworld.CMSGDFSetRoles, []byte{0x00, 4})
	checkWrite(legacyworld.CMSGDFSetRoles, []byte{4})
	if session.lfgRoles != 4 {
		t.Fatalf("cached lfg roles = %d", session.lfgRoles)
	}

	join := []byte{0x00, 8}
	join = binary.LittleEndian.AppendUint32(join, 1)
	join = binary.LittleEndian.AppendUint32(join, 0x45)
	handle(modernworld.CMSGDFJoin, join)
	wantJoin := binary.LittleEndian.AppendUint32(nil, 8)
	wantJoin = append(wantJoin, 0, 0, 1)
	wantJoin = binary.LittleEndian.AppendUint32(wantJoin, 0x45)
	wantJoin = append(wantJoin, 3, 0, 0, 0, 0)
	checkWrite(legacyworld.CMSGLfgJoin, wantJoin)

	leave := []byte{0, 0}
	leave = binary.LittleEndian.AppendUint32(leave, 1)
	leave = binary.LittleEndian.AppendUint32(leave, 2)
	leave = binary.LittleEndian.AppendUint64(leave, 0)
	leave = append(leave, 0)
	handle(modernworld.CMSGDFLeave, leave)
	checkWrite(legacyworld.CMSGLfgLeave, nil)

	proposal := append([]byte(nil), leave...)
	proposal = append(proposal, make([]byte, 8)...)
	proposal = binary.LittleEndian.AppendUint32(proposal, 9)
	proposal = append(proposal, 0x80)
	handle(modernworld.CMSGDFProposalResponse, proposal)
	checkWrite(legacyworld.CMSGLfgProposalResult, []byte{9, 0, 0, 0, 1})

	handle(modernworld.CMSGDFGetSystemInfo, []byte{0x80})
	checkWrite(legacyworld.CMSGLfdPlayerLockInfoRequest, nil)
	handle(modernworld.CMSGDFGetSystemInfo, []byte{0x00})
	checkWrite(legacyworld.CMSGLfdPartyLockInfoRequest, nil)
	handle(modernworld.CMSGDFGetJoinStatus, nil)
	checkWrite(legacyworld.CMSGLfgGetStatus, nil)
	handle(modernworld.CMSGDFBootPlayerVote, []byte{0x80})
	checkWrite(legacyworld.CMSGLfgSetBootVote, []byte{1})
	handle(modernworld.CMSGDFTeleport, []byte{0x00})
	checkWrite(legacyworld.CMSGLfgTeleport, []byte{0})

	changeSubgroup := appendTestModernPackedGUID(nil, target)
	changeSubgroup = append(changeSubgroup, 7, 0) // subgroup followed by hasParty=false bit byte
	handle(modernworld.CMSGChangeSubGroup, changeSubgroup)
	checkWrite(legacyworld.CMSGGroupChangeSubGroup, []byte("Bob\x00\x07"))

	swapSubgroups := appendTestModernPackedGUID([]byte{0}, self)
	swapSubgroups = appendTestModernPackedGUID(swapSubgroups, target)
	handle(modernworld.CMSGSwapSubGroups, swapSubgroups)
	checkWrite(legacyworld.CMSGGroupSwapSubGroup, []byte("Alice\x00Bob\x00"))

	ping := []byte{0}
	ping = binary.LittleEndian.AppendUint32(ping, math.Float32bits(0.25))
	ping = binary.LittleEndian.AppendUint32(ping, math.Float32bits(0.75))
	handle(modernworld.CMSGMinimapPing, ping)
	checkWrite(legacyworld.MSGMinimapPing, ping[1:])

	roll := binary.LittleEndian.AppendUint32([]byte{0}, 1)
	roll = binary.LittleEndian.AppendUint32(roll, 100)
	handle(modernworld.CMSGRandomRoll, roll)
	checkWrite(legacyworld.MSGRandomRoll, roll[1:])

	lootObject := modernworld.ModernLootGUID(0x500, 0)
	session.lootLegacyGUID = 0x500
	session.lootObjModern = lootObject
	session.partyLootMethod = 2
	session.partyLootMaster = selfLegacy
	session.masterLootCandidates = []uint64{targetLegacy}
	masterLoot := binary.LittleEndian.AppendUint32(nil, 1)
	masterLoot = appendTestModernPackedGUID(masterLoot, target)
	masterLoot = appendTestModernPackedGUID(masterLoot, lootObject)
	masterLoot = append(masterLoot, 3)
	handle(modernworld.CMSGMasterLootItem, masterLoot)
	wantMasterLoot := binary.LittleEndian.AppendUint64(nil, 0x500)
	wantMasterLoot = append(wantMasterLoot, 3)
	wantMasterLoot = binary.LittleEndian.AppendUint64(wantMasterLoot, targetLegacy)
	checkWrite(legacyworld.CMSGLootMasterGive, wantMasterLoot)

	// raid=false has no 3.3.5 equivalent and must not be forwarded as another
	// party->raid conversion.
	handle(modernworld.CMSGConvertRaid, []byte{0})
	if len(legacyConnection.writes) != 0 {
		t.Fatalf("raid-to-party request was incorrectly forwarded: %#v", legacyConnection.writes)
	}
}

func TestLFGStatusRelaysToClient(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:           &legacyauth.Session{Username: "TEST"},
		legacyWorld:      legacyConnection,
		currentCharacter: 0x42,
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGLFGDisabled}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGLFGOfferContinue, Body: binary.LittleEndian.AppendUint32(nil, 0x45)}
	player := []byte{5, 1, 1, 0, 0, 0}
	player = append(player, 0)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGLFGUpdatePlayer, Body: player}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGLFGUpdateParty, Body: []byte{14, 0}}

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 3 {
			break
		}
		if time.Now().After(deadline) {
			session.worldMu.Lock()
			queued = len(session.pendingInstance)
			session.worldMu.Unlock()
			t.Fatalf("lfg relay queued %d packets before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}
	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
	session.worldMu.Lock()
	packets := append([]modernworld.Packet(nil), session.pendingInstance...)
	mode := session.lfgQueueMode
	session.worldMu.Unlock()
	if packets[0].Opcode != modernworld.SMSGLFGDisabled || len(packets[0].Body) != 0 {
		t.Fatalf("disabled opcode=%#x body=%x", packets[0].Opcode, packets[0].Body)
	}
	if packets[1].Opcode != modernworld.SMSGLFGOfferContinue || !bytes.Equal(packets[1].Body, binary.LittleEndian.AppendUint32(nil, 0x45)) {
		t.Fatalf("offer opcode=%#x body=%x", packets[1].Opcode, packets[1].Body)
	}
	if packets[2].Opcode != modernworld.SMSGLFGUpdateStatus {
		t.Fatalf("update opcode=%#x", packets[2].Opcode)
	}
	if mode != modernworld.LFGQueuePlayer {
		t.Fatalf("queue mode = %d", mode)
	}
}

func TestPartyNameQueryWarmupIsDeduplicated(t *testing.T) {
	const memberGUID = uint64(0x43)
	session := &proxySession{}

	session.worldMu.Lock()
	first := session.queuePlayerNameQueryLocked(memberGUID)
	duplicate := session.queuePlayerNameQueryLocked(memberGUID)
	session.worldMu.Unlock()
	if !first || duplicate {
		t.Fatalf("first=%v duplicate=%v", first, duplicate)
	}

	session.worldMu.Lock()
	session.takeNamedChatsLocked(memberGUID)
	session.playerIdentities = map[uint64]modernworld.LegacyNameIdentity{
		memberGUID: {GUID: memberGUID, Name: "Bob", Race: 1, Class: 1},
	}
	known := session.queuePlayerNameQueryLocked(memberGUID)
	session.worldMu.Unlock()
	if known {
		t.Fatal("cached party member unexpectedly queued another name query")
	}
}

func TestQueryPlayerNamesAnswersLocalPlayerWithoutLegacyRoundTrip(t *testing.T) {
	player := modernworld.GUID128{Low: 0x42, High: uint64(2)<<58 | uint64(1)<<42}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:           &legacyauth.Session{Username: "TEST"},
		legacyWorld:      legacyConnection,
		currentCharacter: 0x42,
		objectGUIDs:      map[uint64]modernworld.GUID128{0x42: player},
		knownCharacterInfo: map[uint64]modernworld.LegacyCharacter{
			0x42: {GUID: 0x42, Name: "Topaz", Race: 4, Sex: 1, Class: 3, Level: 80},
		},
	}
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = appendTestModernPackedGUID(body, player)
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGQueryPlayerNames, Body: body})
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	select {
	case write := <-legacyConnection.writes:
		t.Fatalf("local name lookup forwarded to legacy: %#v", write)
	default:
	}
	if len(session.pendingInstance) != 1 || session.pendingInstance[0].Opcode != modernworld.SMSGQueryPlayerNames {
		t.Fatalf("pending name response=%#v", session.pendingInstance)
	}
}

func TestQueryPlayerNamesDoesNotFailCreatureGUIDs(t *testing.T) {
	creature := modernworld.GUID128{Low: 0x43, High: uint64(8)<<58 | uint64(1)<<42}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{0xf130000001000043: creature},
	}
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = appendTestModernPackedGUID(body, creature)
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGQueryPlayerNames, Body: body})
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	select {
	case write := <-legacyConnection.writes:
		t.Fatalf("creature GUID was forwarded as a player name query: %#v", write)
	default:
	}
	if len(session.pendingInstance) != 0 {
		t.Fatalf("creature GUID produced a player-name failure: %#v", session.pendingInstance)
	}
}

func TestRolePollBroadcastsToActiveModernPartySessions(t *testing.T) {
	first := &proxySession{partyLegacyGUID: 0x1001}
	second := &proxySession{partyLegacyGUID: 0x1001}
	other := &proxySession{partyLegacyGUID: 0x2002}
	server := &Server{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		worldSessions: map[string]*proxySession{
			"FIRST":  first,
			"SECOND": second,
			"OTHER":  other,
		},
	}
	packet := modernworld.Packet{Opcode: modernworld.SMSGRolePollInform, Body: []byte{0, 0, 0}}
	if err := server.broadcastPartyPacket(first, packet); err != nil {
		t.Fatal(err)
	}
	if len(first.pendingInstance) != 1 || len(second.pendingInstance) != 1 || len(other.pendingInstance) != 0 {
		t.Fatalf("broadcast counts first=%d second=%d other=%d", len(first.pendingInstance), len(second.pendingInstance), len(other.pendingInstance))
	}
}

func TestNamedChatQueueDeduplicatesAndDrains(t *testing.T) {
	session := &proxySession{}
	message := modernworld.LegacyChatMessage{Type: 7, Sender: 0x43, Receiver: 0x42, Text: "hello"}
	if !session.queueNamedChatLocked(message) {
		t.Fatal("first chat did not request a name query")
	}
	if session.queueNamedChatLocked(message) {
		t.Fatal("second chat requested a duplicate name query")
	}
	session.rememberPlayerNameLocked(0x43, "Alice")
	queued := session.takeNamedChatsLocked(0x43)
	if session.playerNames[0x43] != "Alice" || len(queued) != 2 || len(session.pendingNamedChats) != 0 || len(session.pendingPlayerNameQueries) != 0 {
		t.Fatalf("name=%q queued=%d pending=%d queries=%d", session.playerNames[0x43], len(queued), len(session.pendingNamedChats), len(session.pendingPlayerNameQueries))
	}
}

func TestSocialAccountResolutionUsesViewerAccountForOwnCharacter(t *testing.T) {
	viewer := &proxySession{
		selectedRealm:   legacyauth.Realm{ID: 7},
		gameAccountID:   0x1234,
		knownCharacters: map[uint64]struct{}{0x42: {}},
	}
	server := &Server{}
	want := modernworld.ModernWowAccountGUID(0x1234)
	if got := server.socialWowAccountGUID(viewer, 0x42); got != want {
		t.Fatalf("own character account=%#v want=%#v", got, want)
	}
	if proxyGameAccountID("Alice") != proxyGameAccountID("alice") || proxyGameAccountID("Alice") == proxyGameAccountID("Bob") {
		t.Fatal("proxy game-account IDs are not stable and distinct")
	}
}

func TestSocialAccountResolutionUsesStablePlayerIdentityForFriend(t *testing.T) {
	viewer := &proxySession{selectedRealm: legacyauth.Realm{ID: 7}}
	friend := &proxySession{
		selectedRealm:    legacyauth.Realm{ID: 7},
		currentCharacter: 0x43,
		gameAccountID:    0x1234,
	}
	server := &Server{
		worldSessions:    map[string]*proxySession{"VIEWER": viewer, "FRIEND": friend},
		instanceSessions: make(map[uint64]*proxySession),
	}
	want := modernworld.ModernWowAccountGUIDForLegacy(0x43)
	if got := server.socialWowAccountGUID(viewer, 0x43); got != want {
		t.Fatalf("online friend account=%#v want=%#v", got, want)
	}
	friend.worldMu.Lock()
	friend.currentCharacter = 0
	friend.worldMu.Unlock()
	if got := server.socialWowAccountGUID(viewer, 0x43); got != want {
		t.Fatalf("offline friend account=%#v want=%#v", got, want)
	}
}

func TestResetInstancesForwardsToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGResetInstances})
	if err != nil || !handled {
		t.Fatalf("reset instances handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGResetInstances || len(write.body) != 0 {
		t.Fatalf("unexpected reset-instances write: %#v", write)
	}

	if _, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGResetInstances, Body: []byte{0}}); err == nil {
		t.Fatal("expected malformed reset-instances request error")
	}
}

func TestCancelMountAuraForwardsToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGCancelMountAura})
	if err != nil || !handled {
		t.Fatalf("cancel mount aura handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGCancelMountAura || len(write.body) != 0 {
		t.Fatalf("unexpected cancel-mount-aura write: %#v", write)
	}

	if _, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGCancelMountAura, Body: []byte{0}}); err == nil {
		t.Fatal("expected malformed cancel-mount-aura request error")
	}
}

func TestCancelRemappedMountAuraForwardsDismount(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}

	body := binary.LittleEndian.AppendUint32(nil, 54729)
	body = appendTestModernPackedGUID(body, modernworld.GUID128{Low: 1, High: 1})
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGCancelAura, Body: body})
	if err != nil || !handled {
		t.Fatalf("cancel remapped mount handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGCancelMountAura || len(write.body) != 0 {
		t.Fatalf("right-clicking remapped mount should dismount, got %#v", write)
	}

	ordinary := binary.LittleEndian.AppendUint32(nil, 48266)
	ordinary = appendTestModernPackedGUID(ordinary, modernworld.GUID128{Low: 1, High: 1})
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGCancelAura, Body: ordinary})
	if err != nil || !handled {
		t.Fatalf("cancel ordinary aura handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGCancelAura || binary.LittleEndian.Uint32(write.body) != 48266 {
		t.Fatalf("ordinary aura cancel was rewritten: %#v", write)
	}
}

func TestUpdateMissileTrajectoryForwardsToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	modernPlayer := modernworld.ModernGUIDForLegacy(0x42, 0)
	session := &proxySession{
		legacy:           &legacyauth.Session{Username: "TEST"},
		legacyWorld:      legacyConnection,
		currentCharacter: 0x42,
		objectGUIDs:      map[uint64]modernworld.GUID128{0x42: modernPlayer},
	}
	request := modernworld.UpdateMissileTrajectory{
		Guid:      modernPlayer,
		CastID:    modernworld.ModernCastGUID(0, 7268, 1),
		MoveMsgID: 1,
		SpellID:   7268,
		Pitch:     0.5,
		Speed:     20,
		FirePos:   modernworld.Vec3{X: 1, Y: 2, Z: 3},
		ImpactPos: modernworld.Vec3{X: 4, Y: 5, Z: 6},
	}
	modernBody := appendTestModernPackedGUID(nil, request.Guid)
	modernBody = appendTestModernPackedGUID(modernBody, request.CastID)
	modernBody = binary.LittleEndian.AppendUint16(modernBody, request.MoveMsgID)
	modernBody = binary.LittleEndian.AppendUint32(modernBody, request.SpellID)
	for _, value := range []float32{request.Pitch, request.Speed, request.FirePos.X, request.FirePos.Y, request.FirePos.Z, request.ImpactPos.X, request.ImpactPos.Y, request.ImpactPos.Z} {
		modernBody = binary.LittleEndian.AppendUint32(modernBody, math.Float32bits(value))
	}

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGUpdateMissileTrajectory, Body: modernBody})
	if err != nil || !handled {
		t.Fatalf("update missile trajectory handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGUpdateMissileTrajectory {
		t.Fatalf("unexpected missile-trajectory opcode %#x", write.opcode)
	}
	want := modernworld.EncodeLegacyUpdateMissileTrajectory(0x42, request)
	if !bytes.Equal(write.body, want) {
		t.Fatalf("legacy missile trajectory %x want %x", write.body, want)
	}
}

func TestSetAmmoForwardsToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}
	body := binary.LittleEndian.AppendUint32(nil, 0x12345678)

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGSetAmmo, Body: body})
	if err != nil || !handled {
		t.Fatalf("set ammo handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGSetAmmo || !bytes.Equal(write.body, body) {
		t.Fatalf("unexpected set-ammo write: %#v", write)
	}

	if _, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGSetAmmo, Body: []byte{1, 2, 3}}); err == nil {
		t.Fatal("expected malformed set-ammo request error")
	}
}

func TestAcceptQuestRefreshesQuestGiverStatus(t *testing.T) {
	const (
		legacyGiver = uint64(0xf1300001d3000288)
		questID     = uint32(155)
	)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	modernGiver := modernworld.GUID128{Low: 0x42}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyGiver: modernGiver},
		objectTypes: map[uint64]uint8{legacyGiver: 3},
	}
	// PackedGuid128(low=0x42, high=0), quest ID, StartCheat=false bit.
	body := []byte{0x01, 0x00, 0x42}
	body = binary.LittleEndian.AppendUint32(body, questID)
	body = append(body, 0)

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{
		Opcode: modernworld.CMSGQuestGiverAcceptQuest,
		Body:   body,
	})
	if err != nil || !handled {
		t.Fatalf("accept quest handled=%v err=%v", handled, err)
	}
	accept := legacyConnection.nextWrite(t)
	if accept.opcode != legacyworld.CMSGQuestGiverAcceptQuest || len(accept.body) != 16 ||
		binary.LittleEndian.Uint64(accept.body) != legacyGiver || binary.LittleEndian.Uint32(accept.body[8:]) != questID {
		t.Fatalf("unexpected legacy accept request: %#v", accept)
	}
	refresh := legacyConnection.nextWrite(t)
	if refresh.opcode != legacyworld.CMSGQuestGiverStatusQuery ||
		!bytes.Equal(refresh.body, modernworld.EncodeLegacyUnpackedGUID(legacyGiver)) {
		t.Fatalf("unexpected post-accept status refresh: %#v", refresh)
	}
}

func TestQuestShareClientOpcodesForward(t *testing.T) {
	const (
		legacySender = uint64(0x43)
		questID      = uint32(12416)
	)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	modernSender := modernworld.GUID128{Low: legacySender, High: uint64(2) << 58}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacySender: modernSender},
	}

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{
		Opcode: modernworld.CMSGQuestConfirmAccept,
		Body:   modernworld.EncodeLegacyQuestID(questID),
	})
	if err != nil || !handled {
		t.Fatalf("confirm accept handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGQuestConfirmAccept || !bytes.Equal(write.body, modernworld.EncodeLegacyQuestID(questID)) {
		t.Fatalf("unexpected legacy confirm-accept: %#v", write)
	}

	body := appendTestModernPackedGUID(nil, modernSender)
	body = binary.LittleEndian.AppendUint32(body, questID)
	body = append(body, 6) // 54261 Dead; AC has no Dead, so Busy=4
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{
		Opcode: modernworld.CMSGQuestPushResult,
		Body:   body,
	})
	if err != nil || !handled {
		t.Fatalf("quest push result handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != uint32(legacyworld.MSGQuestPushResult) || len(write.body) != 13 {
		t.Fatalf("unexpected legacy quest-push-result: %#v", write)
	}
	if binary.LittleEndian.Uint64(write.body[:8]) != legacySender || binary.LittleEndian.Uint32(write.body[8:12]) != questID || write.body[12] != 4 {
		t.Fatalf("legacy quest-push-result body %x", write.body)
	}
}

func TestCancelTradeSuppressedUntilActivePlayerExists(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGCancelTrade})
	if err != nil || !handled {
		t.Fatalf("pre-login cancel trade handled=%v err=%v", handled, err)
	}
	if len(legacyConnection.writes) != 0 {
		t.Fatal("pre-login cancel trade was forwarded")
	}

	session.activePlayerCreated = true
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGCancelTrade})
	if err != nil || !handled {
		t.Fatalf("logged-in cancel trade handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGCancelTrade || len(write.body) != 0 {
		t.Fatalf("unexpected cancel-trade write: %#v", write)
	}
}

func TestRepopRequestForwardsRequiredLegacyByte(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGRepopRequest, Body: []byte{0x80}})
	if err != nil || !handled {
		t.Fatalf("repop request handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGRepopRequest || !bytes.Equal(write.body, []byte{1}) {
		t.Fatalf("unexpected repop write: %#v", write)
	}
}

func TestLogoutAndAreaTriggerForwardToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}

	requests := []struct {
		packet modernworld.Packet
		opcode uint32
		body   []byte
	}{
		{packet: modernworld.Packet{Opcode: modernworld.CMSGLogoutRequest, Body: []byte{1}}, opcode: legacyworld.CMSGLogoutRequest},
		{packet: modernworld.Packet{Opcode: modernworld.CMSGLogoutCancel}, opcode: legacyworld.CMSGLogoutCancel},
		{
			packet: modernworld.Packet{Opcode: modernworld.CMSGAreaTrigger, Body: []byte{0x78, 0x56, 0x34, 0x12, 0xC0}},
			opcode: legacyworld.CMSGAreaTrigger,
			body:   []byte{0x78, 0x56, 0x34, 0x12},
		},
	}
	for _, request := range requests {
		handled, err := server.handleModernPlayPacket(session, nil, request.packet)
		if err != nil {
			t.Fatal(err)
		}
		if !handled {
			t.Fatalf("opcode 0x%x was not handled", request.packet.Opcode)
		}
		write := legacyConnection.nextWrite(t)
		if write.opcode != request.opcode || !bytes.Equal(write.body, request.body) {
			t.Fatalf("opcode 0x%x forwarded as %#v", request.packet.Opcode, write)
		}
	}
}

func TestAreaTriggerExitDoesNotTeleport(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}
	for _, flags := range []byte{0, 0x40} {
		handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGAreaTrigger, Body: []byte{2, 17, 0, 0, flags}})
		if !handled || err != nil {
			t.Fatalf("handled=%v err=%v", handled, err)
		}
		select {
		case packet := <-legacyConnection.writes:
			t.Fatalf("exit forwarded: %+v", packet)
		default:
		}
	}
}

func TestCharacterSettingsForwardToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}

	requests := []struct {
		name   string
		packet modernworld.Packet
		opcode uint32
		body   []byte
	}{
		{
			name:   "set title",
			packet: modernworld.Packet{Opcode: modernworld.CMSGSetTitle, Body: binary.LittleEndian.AppendUint32(nil, 42)},
			opcode: legacyworld.CMSGSetTitle,
			body:   binary.LittleEndian.AppendUint32(nil, 42),
		},
		{
			name:   "toggle pvp",
			packet: modernworld.Packet{Opcode: modernworld.CMSGTogglePvP},
			opcode: legacyworld.CMSGTogglePvP,
			body:   nil,
		},
		{
			name:   "set pvp on",
			packet: modernworld.Packet{Opcode: modernworld.CMSGSetPvP, Body: []byte{0x80}},
			opcode: legacyworld.CMSGTogglePvP,
			body:   []byte{1},
		},
		{
			name:   "set pvp off",
			packet: modernworld.Packet{Opcode: modernworld.CMSGSetPvP, Body: []byte{0}},
			opcode: legacyworld.CMSGTogglePvP,
			body:   []byte{0},
		},
		{
			name:   "unlearn skill",
			packet: modernworld.Packet{Opcode: modernworld.CMSGUnlearnSkill, Body: binary.LittleEndian.AppendUint32(nil, 171)},
			opcode: legacyworld.CMSGUnlearnSkill,
			body:   binary.LittleEndian.AppendUint32(nil, 171),
		},
		{
			name:   "remove glyph",
			packet: modernworld.Packet{Opcode: modernworld.CMSGRemoveGlyph, Body: []byte{4}},
			opcode: legacyworld.CMSGRemoveGlyph,
			body:   binary.LittleEndian.AppendUint32(nil, 4),
		},
	}
	for _, request := range requests {
		handled, err := server.handleModernPlayPacket(session, nil, request.packet)
		if err != nil || !handled {
			t.Fatalf("%s handled=%v err=%v", request.name, handled, err)
		}
		write := legacyConnection.nextWrite(t)
		if write.opcode != request.opcode || !bytes.Equal(write.body, request.body) {
			t.Fatalf("%s forwarded as %#v, want opcode=0x%x body=%x", request.name, write, request.opcode, request.body)
		}
	}

	// A pre-world session with no legacy connection consumes the control.
	pre := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}}
	handled, err := server.handleModernPlayPacket(pre, nil, modernworld.Packet{Opcode: modernworld.CMSGSetTitle, Body: binary.LittleEndian.AppendUint32(nil, 1)})
	if err != nil || !handled {
		t.Fatalf("pre-world set title handled=%v err=%v", handled, err)
	}

	// Malformed bodies are rejected rather than forwarded.
	for _, bad := range []struct {
		name   string
		packet modernworld.Packet
	}{
		{name: "short title", packet: modernworld.Packet{Opcode: modernworld.CMSGSetTitle, Body: []byte{1}}},
		{name: "nonempty toggle", packet: modernworld.Packet{Opcode: modernworld.CMSGTogglePvP, Body: []byte{1}}},
		{name: "empty set-pvp", packet: modernworld.Packet{Opcode: modernworld.CMSGSetPvP}},
		{name: "short unlearn", packet: modernworld.Packet{Opcode: modernworld.CMSGUnlearnSkill, Body: []byte{1, 2}}},
		{name: "empty glyph", packet: modernworld.Packet{Opcode: modernworld.CMSGRemoveGlyph}},
	} {
		if _, err := server.handleModernPlayPacket(session, nil, bad.packet); err == nil {
			t.Fatalf("%s: expected malformed-body error", bad.name)
		}
	}
}

func TestChatStatusMessagesForwardToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}

	// 11-bit MSB-first length prefix, then the status text.
	statusText := "back in 5"
	body := []byte{0x01, 0x20}
	body = append(body, []byte(statusText)...)

	for _, tc := range []struct {
		opcode  uint16
		msgType uint32
	}{
		{modernworld.CMSGChatMessageAFK, 0x17},
		{modernworld.CMSGChatMessageDND, 0x18},
	} {
		handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: tc.opcode, Body: body})
		if err != nil || !handled {
			t.Fatalf("status opcode=%d handled=%v err=%v", tc.opcode, handled, err)
		}
		write := legacyConnection.nextWrite(t)
		if write.opcode != legacyworld.CMSGMessageChat {
			t.Fatalf("status opcode=%d forwarded as opcode %d", tc.opcode, write.opcode)
		}
		want := binary.LittleEndian.AppendUint32(nil, tc.msgType)
		want = binary.LittleEndian.AppendUint32(want, 0) // LANG_UNIVERSAL is valid for AFK/DND.
		want = append(want, statusText...)
		want = append(want, 0)
		if !bytes.Equal(write.body, want) {
			t.Fatalf("status opcode=%d body=%x", tc.opcode, write.body)
		}
	}

	// A malformed status body is rejected rather than forwarded.
	if _, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGChatMessageAFK, Body: []byte{0x05, 0x00, 'a'}}); err == nil {
		t.Fatal("expected truncated status text to error")
	}
}

func TestChannelMessageAndFarSightForwardToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}

	// Language, empty packed ChannelGUID, 9-bit channel length (7), 11-bit
	// text length (5), secure-flag absent, followed by the two strings.
	channelBody := binary.LittleEndian.AppendUint32(nil, 0)
	channelBody = append(channelBody, 0, 0, 0x03, 0x80, 0x50)
	channelBody = append(channelBody, "Generalhello"...)
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{
		Opcode: modernworld.CMSGChatMessageChannel,
		Body:   channelBody,
	})
	if err != nil || !handled {
		t.Fatalf("channel message handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGMessageChat || !bytes.Equal(write.body, []byte{17, 0, 0, 0, 0, 0, 0, 0, 'G', 'e', 'n', 'e', 'r', 'a', 'l', 0, 'h', 'e', 'l', 'l', 'o', 0}) {
		t.Fatalf("channel message forwarded as opcode=%#x body=%x", write.opcode, write.body)
	}

	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{
		Opcode: modernworld.CMSGFarSight,
		Body:   []byte{0x80},
	})
	if err != nil || !handled {
		t.Fatalf("far sight handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGFarSight || !bytes.Equal(write.body, []byte{1}) {
		t.Fatalf("far sight forwarded as opcode=%#x body=%x", write.opcode, write.body)
	}
}

func TestBankerControlsForwardToLegacy(t *testing.T) {
	legacyNPC := uint64(0xf130005100001234)
	modernNPC := modernworld.GUID128{Low: 0x1234, High: uint64(3) << 58}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyNPC: modernNPC},
	}

	npcBody := appendTestModernPackedGUID(nil, modernNPC)
	for _, request := range []struct {
		name   string
		opcode uint16
		legacy uint32
	}{
		{name: "banker activate", opcode: modernworld.CMSGBankerActivate, legacy: legacyworld.CMSGBankerActivate},
		{name: "buy bank slot", opcode: modernworld.CMSGBuyBankSlot, legacy: legacyworld.CMSGBuyBankSlot},
	} {
		handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: request.opcode, Body: npcBody})
		if err != nil || !handled {
			t.Fatalf("%s handled=%v err=%v", request.name, handled, err)
		}
		write := legacyConnection.nextWrite(t)
		if write.opcode != request.legacy || binary.LittleEndian.Uint64(write.body) != legacyNPC {
			t.Fatalf("%s write=%#v", request.name, write)
		}
	}

	// Bank deposit/withdraw reuse the auto-equip container+slot body. The modern
	// header is a 2-bit inv-update count (0) followed by bag/slot bytes.
	invBody := []byte{0x00, 0x04, 0x05}
	for _, request := range []struct {
		name   string
		opcode uint16
		legacy uint32
	}{
		{name: "deposit", opcode: modernworld.CMSGAutobankItem, legacy: legacyworld.CMSGAutobankItem},
		{name: "withdraw", opcode: modernworld.CMSGAutostoreBankItem, legacy: legacyworld.CMSGAutostoreBankItem},
	} {
		handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: request.opcode, Body: invBody})
		if err != nil || !handled {
			t.Fatalf("%s handled=%v err=%v", request.name, handled, err)
		}
		write := legacyConnection.nextWrite(t)
		if write.opcode != request.legacy || !bytes.Equal(write.body, []byte{0x04, 0x05}) {
			t.Fatalf("%s write=%#v", request.name, write)
		}
	}

	// An unknown banker NPC is rejected rather than forwarded.
	unknown := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}
	handled, err := server.handleModernPlayPacket(unknown, nil, modernworld.Packet{Opcode: modernworld.CMSGBankerActivate, Body: appendTestModernPackedGUID(nil, modernworld.GUID128{Low: 0x9999, High: uint64(3) << 58})})
	if !handled || err == nil {
		t.Fatalf("unknown banker handled=%v err=%v", handled, err)
	}
}

func TestTalkToGossipOnCannonForwardsSpellClick(t *testing.T) {
	legacyCannon := uint64(0xf150008fe600023f)
	modernCannon := modernworld.GUID128{Low: 0x23f, High: uint64(5) << 58}
	legacyGossip := uint64(0xf130009140000251)
	modernGossip := modernworld.GUID128{Low: 0x251, High: uint64(3) << 58}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{
			legacyCannon: modernCannon,
			legacyGossip: modernGossip,
		},
	}

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{
		Opcode: modernworld.CMSGTalkToGossip,
		Body:   appendTestModernPackedGUID(nil, modernCannon),
	})
	if err != nil || !handled {
		t.Fatalf("cannon gossip handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGSpellClick || binary.LittleEndian.Uint64(write.body) != legacyCannon {
		t.Fatalf("cannon write=%#v", write)
	}

	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{
		Opcode: modernworld.CMSGTalkToGossip,
		Body:   appendTestModernPackedGUID(nil, modernGossip),
	})
	if err != nil || !handled {
		t.Fatalf("npc gossip handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGGossipHello || binary.LittleEndian.Uint64(write.body) != legacyGossip {
		t.Fatalf("npc write=%#v", write)
	}
}

func TestConfirmRespecWipeForwardsTalentWipe(t *testing.T) {
	legacyNPC := uint64(0xf130001234000042)
	modernNPC := modernworld.GUID128{Low: 0x42, High: uint64(3) << 58}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyNPC: modernNPC},
	}
	body := appendTestModernPackedGUID(nil, modernNPC)
	body = append(body, modernworld.SpecResetTalents)
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGConfirmRespecWipe, Body: body})
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != uint32(legacyworld.MSGTalentWipeConfirm) || binary.LittleEndian.Uint64(write.body) != legacyNPC {
		t.Fatalf("write=%#v", write)
	}
}

func TestShowBankRelaysToClient(t *testing.T) {
	const legacyBanker = uint64(0xf130005100002345)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyBanker: {Low: 0x2345, High: uint64(3) << 58}},
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	legacyConnection.reads <- legacyworld.Packet{
		Opcode: legacyworld.SMSGShowBank,
		Body:   binary.LittleEndian.AppendUint64(nil, legacyBanker),
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("show-bank queued %d packets before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}

	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
	session.worldMu.Lock()
	packet := session.pendingInstance[0]
	session.worldMu.Unlock()
	if packet.Opcode != modernworld.SMSGNpcInteractionOpenResult {
		t.Fatalf("show-bank modern opcode=%d", packet.Opcode)
	}
	want := modernworld.EncodeNpcInteraction(modernworld.GUID128{Low: 0x2345, High: uint64(3) << 58}, modernworld.PlayerInteractionBanker, true)
	if !bytes.Equal(packet.Body, want) {
		t.Fatalf("show-bank body=%x want=%x", packet.Body, want)
	}
}

func TestAuctionHelloRelaysToClient(t *testing.T) {
	const legacyAuctioneer = uint64(0xf130005100002345)
	modernAuctioneer := modernworld.GUID128{Low: 0x2345, High: uint64(3) << 58}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyAuctioneer: modernAuctioneer},
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	helloBody := binary.LittleEndian.AppendUint64(nil, legacyAuctioneer)
	helloBody = binary.LittleEndian.AppendUint32(helloBody, 3) // auction house id, ignored
	helloBody = append(helloBody, 1)                           // open for business
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.MsgAuctionHello, Body: helloBody}

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("auction hello queued %d packets before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}
	session.worldMu.Lock()
	pending := session.pendingInstance
	session.worldMu.Unlock()
	if pending[0].Opcode != modernworld.SMSGNpcInteractionOpenResult {
		t.Fatalf("first modern packet opcode=%d", pending[0].Opcode)
	}
	wantInteraction := modernworld.EncodeNpcInteraction(modernAuctioneer, modernworld.PlayerInteractionAuctioneer, true)
	if !bytes.Equal(pending[0].Body, wantInteraction) {
		t.Fatalf("interaction body=%x want=%x", pending[0].Body, wantInteraction)
	}
	if pending[1].Opcode != modernworld.SMSGAuctionHelloResponse {
		t.Fatalf("second modern packet opcode=%d", pending[1].Opcode)
	}
	wantHello := modernworld.EncodeAuctionHelloResponse(modernAuctioneer, true)
	if !bytes.Equal(pending[1].Body, wantHello) {
		t.Fatalf("hello response body=%x want=%x", pending[1].Body, wantHello)
	}
	// The relay must answer its own hello with an owned-items listing query.
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGAuctionListOwnedItems || binary.LittleEndian.Uint64(write.body) != legacyAuctioneer {
		t.Fatalf("owned writeback write=%#v", write)
	}
	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
}

func TestAuctionControlsForwardToLegacy(t *testing.T) {
	const (
		legacyAuctioneer = uint64(0xf130005100002345)
		legacyItem       = uint64(0x4000000000000681)
	)
	modernAuctioneer := modernworld.GUID128{Low: 0x2345, High: uint64(3) << 58}
	modernItem := modernworld.GUID128{Low: 0x681, High: uint64(1) << 58}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{
			legacyAuctioneer: modernAuctioneer,
			legacyItem:       modernItem,
		},
	}

	// Hello request forwards to legacy MSG_AUCTION_HELLO with the raw auctioneer guid.
	helloBody := appendTestModernPackedGUID(nil, modernAuctioneer)
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGAuctionHelloRequest, Body: helloBody})
	if err != nil || !handled {
		t.Fatalf("hello handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != uint32(legacyworld.MsgAuctionHello) || binary.LittleEndian.Uint64(write.body) != legacyAuctioneer {
		t.Fatalf("hello write=%#v", write)
	}

	// Owned-items query forwards guid + offset.
	ownedBody := appendTestModernPackedGUID(nil, modernAuctioneer)
	ownedBody = binary.LittleEndian.AppendUint32(ownedBody, 25)
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGAuctionListOwnedItems, Body: ownedBody})
	if err != nil || !handled {
		t.Fatalf("owned handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGAuctionListOwnedItems || binary.LittleEndian.Uint64(write.body) != legacyAuctioneer {
		t.Fatalf("owned write=%#v", write)
	}

	// Sell request forwards the count-first 3.3.5a body with resolved item guid.
	sellBody := appendTestModernPackedGUID(nil, modernAuctioneer)
	sellBody = binary.LittleEndian.AppendUint64(sellBody, 0x1000)
	sellBody = binary.LittleEndian.AppendUint64(sellBody, 0x5000)
	sellBody = binary.LittleEndian.AppendUint32(sellBody, 1440)
	sellBody = append(sellBody, 0x02) // no addon presence bit + one item (6-bit count)
	sellBody = appendTestModernPackedGUID(sellBody, modernItem)
	sellBody = binary.LittleEndian.AppendUint32(sellBody, 5)
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGAuctionSellItem, Body: sellBody})
	if err != nil || !handled {
		t.Fatalf("sell handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGAuctionSellItem {
		t.Fatalf("sell opcode write=%#v", write)
	}
	if binary.LittleEndian.Uint64(write.body[0:]) != legacyAuctioneer ||
		binary.LittleEndian.Uint32(write.body[8:]) != 1 ||
		binary.LittleEndian.Uint64(write.body[12:]) != legacyItem ||
		binary.LittleEndian.Uint32(write.body[20:]) != 5 ||
		binary.LittleEndian.Uint32(write.body[24:]) != 0x1000 ||
		binary.LittleEndian.Uint32(write.body[28:]) != 0x5000 ||
		binary.LittleEndian.Uint32(write.body[32:]) != 1440 {
		t.Fatalf("sell write=%#v", write)
	}
}

func TestAuctionSellBagItemForwardsWithDerivedGUID(t *testing.T) {
	// A weapon sitting in a bag never gets a world create, so its modern GUID128
	// is not in objectGUIDs; the sell must recover the legacy item guid from the
	// deterministic modern-item mapping instead of forwarding zero.
	const legacyAuctioneer = uint64(0xf130005100002345)
	const legacyBagItem = uint64(0x4000000000000819)
	modernAuctioneer := modernworld.GUID128{Low: 0x2345, High: uint64(3) << 58}
	modernBagItem := modernworld.ModernGUIDForLegacy(legacyBagItem, 0)
	if legacyBagItem != modernworld.LegacyItemGUIDFromModern(modernBagItem) {
		t.Fatalf("fixture item does not round trip")
	}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		// Only the auctioneer is a known object; the item is deliberately absent.
		objectGUIDs: map[uint64]modernworld.GUID128{legacyAuctioneer: modernAuctioneer},
	}

	sellBody := appendTestModernPackedGUID(nil, modernAuctioneer)
	sellBody = binary.LittleEndian.AppendUint64(sellBody, 0x1000)
	sellBody = binary.LittleEndian.AppendUint64(sellBody, 0x5000)
	sellBody = binary.LittleEndian.AppendUint32(sellBody, 1440)
	sellBody = append(sellBody, 0x02) // no addon, one item (6-bit count)
	sellBody = appendTestModernPackedGUID(sellBody, modernBagItem)
	sellBody = binary.LittleEndian.AppendUint32(sellBody, 1)

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGAuctionSellItem, Body: sellBody})
	if err != nil || !handled {
		t.Fatalf("sell handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGAuctionSellItem {
		t.Fatalf("sell opcode write=%#v", write)
	}
	if binary.LittleEndian.Uint64(write.body[12:]) != legacyBagItem {
		t.Fatalf("bag-item legacy guid not derived: write=%#v", write)
	}
}

func TestAuctionListResultRelaysToClient(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	entry := binary.LittleEndian.AppendUint32(nil, 1) // auction id
	entry = binary.LittleEndian.AppendUint32(entry, 0x1234)
	for slot := 0; slot < 7; slot++ {
		entry = binary.LittleEndian.AppendUint32(entry, 0)
		entry = binary.LittleEndian.AppendUint32(entry, 0)
		entry = binary.LittleEndian.AppendUint32(entry, 0)
	}
	entry = binary.LittleEndian.AppendUint32(entry, 0)
	entry = binary.LittleEndian.AppendUint32(entry, 0)
	entry = binary.LittleEndian.AppendUint32(entry, 1)
	entry = binary.LittleEndian.AppendUint32(entry, 0)
	entry = binary.LittleEndian.AppendUint32(entry, 0)
	entry = binary.LittleEndian.AppendUint64(entry, 0)
	entry = binary.LittleEndian.AppendUint32(entry, 0x100)
	entry = binary.LittleEndian.AppendUint32(entry, 50)
	entry = binary.LittleEndian.AppendUint32(entry, 0)
	entry = binary.LittleEndian.AppendUint32(entry, 3600)
	entry = binary.LittleEndian.AppendUint64(entry, 0)
	entry = binary.LittleEndian.AppendUint32(entry, 0)
	legacyBody := binary.LittleEndian.AppendUint32(nil, 1)
	legacyBody = append(legacyBody, entry...)
	legacyBody = binary.LittleEndian.AppendUint32(legacyBody, 1)   // total items
	legacyBody = binary.LittleEndian.AppendUint32(legacyBody, 300) // desired delay
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGAuctionListItemsResult, Body: legacyBody}

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("auction list result queued %d packets before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}
	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
	session.worldMu.Lock()
	packet := session.pendingInstance[0]
	session.worldMu.Unlock()
	if packet.Opcode != modernworld.SMSGAuctionListItemsResult {
		t.Fatalf("modern list result opcode=%d", packet.Opcode)
	}
	if len(packet.Body) < 13 || binary.LittleEndian.Uint32(packet.Body) != 1 {
		t.Fatalf("modern list result head=%x", packet.Body)
	}
}

func TestInspectPvpForwardsToLegacy(t *testing.T) {
	const legacyPlayer = uint64(0xf130000001000043)
	modernPlayer := modernworld.GUID128{Low: 0x43, High: uint64(1) << 58}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyPlayer: modernPlayer},
	}
	body := appendTestModernPackedGUID(nil, modernPlayer)
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGInspectPvp, Body: body})
	if err != nil || !handled {
		t.Fatalf("inspect-pvp handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != uint32(legacyworld.MsgInspectArenaTeams) || binary.LittleEndian.Uint64(write.body) != legacyPlayer {
		t.Fatalf("inspect-pvp write=%#v", write)
	}
}

func TestTitleAndHonorRelayToClient(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	// Title earned.
	titleBody := binary.LittleEndian.AppendUint32(nil, 77)
	titleBody = binary.LittleEndian.AppendUint32(titleBody, 1)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGTitleEarned, Body: titleBody}
	// Honor inspect reply.
	honorBody := binary.LittleEndian.AppendUint64(nil, 0xf130000001000043)
	honorBody = append(honorBody, 7)
	honorBody = binary.LittleEndian.AppendUint16(honorBody, 3)
	honorBody = binary.LittleEndian.AppendUint16(honorBody, 9)
	honorBody = binary.LittleEndian.AppendUint32(honorBody, 10)
	honorBody = binary.LittleEndian.AppendUint32(honorBody, 20)
	honorBody = binary.LittleEndian.AppendUint32(honorBody, 150)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.MsgInspectHonorStats, Body: honorBody}

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("title/honor queued %d packets before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}
	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
	session.worldMu.Lock()
	pending := session.pendingInstance
	session.worldMu.Unlock()
	if pending[0].Opcode != modernworld.SMSGTitleEarned || len(pending[0].Body) != 4 || binary.LittleEndian.Uint32(pending[0].Body) != 77 {
		t.Fatalf("title packet=%#v", pending[0])
	}
	if pending[1].Opcode != modernworld.SMSGInspectHonorStats {
		t.Fatalf("honor packet opcode=%d", pending[1].Opcode)
	}
	if len(pending[1].Body) < 12 || pending[1].Body[len(pending[1].Body)-1] != 0 {
		t.Fatalf("honor packet body=%x", pending[1].Body)
	}
}

func TestItemEnchantTimeRelaysToClient(t *testing.T) {
	const (
		legacyItem  = uint64(0x4000000000000681)
		legacyOwner = uint64(0xf130000001000043)
	)
	modernItem := modernworld.GUID128{Low: 0x681, High: uint64(1) << 58}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyItem: modernItem},
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	legacyBody := make([]byte, 24)
	binary.LittleEndian.PutUint64(legacyBody[0:8], legacyItem)
	binary.LittleEndian.PutUint32(legacyBody[8:12], 3) // slot
	binary.LittleEndian.PutUint32(legacyBody[12:16], 1800)
	binary.LittleEndian.PutUint64(legacyBody[16:24], legacyOwner)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGItemEnchantTimeUpdate, Body: legacyBody}

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("item-enchant-time queued %d packets before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}

	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
	session.worldMu.Lock()
	packet := session.pendingInstance[0]
	session.worldMu.Unlock()
	if packet.Opcode != modernworld.SMSGItemEnchantTimeUpdate {
		t.Fatalf("item-enchant-time modern opcode=%d", packet.Opcode)
	}
	update, err := modernworld.ParseLegacyItemEnchantTime(legacyBody)
	if err != nil {
		t.Fatal(err)
	}
	// The owner GUID was not in the object cache, so the relay synthesizes one
	// from the map id (0 in this test) exactly like the production path.
	owner := modernworld.ModernGUIDForLegacy(legacyOwner, 0)
	want := modernworld.EncodeItemEnchantTime(update, modernItem, owner)
	if !bytes.Equal(packet.Body, want) {
		t.Fatalf("item-enchant-time body=%x want=%x", packet.Body, want)
	}
}

func TestEnchantmentLogRelaysToClient(t *testing.T) {
	const (
		legacyOwner = uint64(0x42)
		legacyItem  = uint64(0x4000000000000681)
		itemID      = uint32(2862)
		enchantID   = uint32(2830)
	)
	modernItem := modernworld.GUID128{Low: 0x681, High: uint64(1) << 58}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:           &legacyauth.Session{Username: "TEST"},
		legacyWorld:      legacyConnection,
		currentCharacter: legacyOwner,
		objectGUIDs:      map[uint64]modernworld.GUID128{legacyItem: modernItem, legacyOwner: {Low: 0x42}},
		objectFields: map[uint64]map[int]uint32{
			legacyOwner: {
				324: uint32(legacyItem & 0xffffffff),
				325: uint32(legacyItem >> 32),
			},
			legacyItem: {3: itemID, 14: 1},
		},
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	legacyBody := modernworld.EncodeLegacyPackedGUID(legacyOwner)
	legacyBody = append(legacyBody, modernworld.EncodeLegacyPackedGUID(0)...)
	legacyBody = binary.LittleEndian.AppendUint32(legacyBody, itemID)
	legacyBody = binary.LittleEndian.AppendUint32(legacyBody, enchantID)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGEnchantmentLog, Body: legacyBody}

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("enchantment-log queued %d packets before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}

	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
	session.worldMu.Lock()
	packet := session.pendingInstance[0]
	session.worldMu.Unlock()
	if packet.Opcode != modernworld.SMSGEnchantmentLog {
		t.Fatalf("enchantment-log modern opcode=%d", packet.Opcode)
	}
	parsed, err := modernworld.ParseLegacyEnchantmentLog(legacyBody)
	if err != nil {
		t.Fatal(err)
	}
	want := modernworld.EncodeEnchantmentLog(parsed, modernworld.GUID128{Low: 0x42}, modernworld.GUID128{}, modernItem)
	if !bytes.Equal(packet.Body, want) {
		t.Fatalf("enchantment-log body=%x want=%x", packet.Body, want)
	}
}

func TestItemCooldownAndDeathRelaysToClient(t *testing.T) {
	const legacyItem = uint64(0x4000000000000681)
	modernItem := modernworld.GUID128{Low: 0x681, High: uint64(1) << 58}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyItem: modernItem},
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	cooldownBody := make([]byte, 12)
	binary.LittleEndian.PutUint64(cooldownBody[0:8], legacyItem)
	binary.LittleEndian.PutUint32(cooldownBody[8:12], 2828)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGItemCooldown, Body: cooldownBody}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGDurabilityDamageDeath, Body: nil}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGSocketGems, Body: make([]byte, 8)}

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("item cooldown/death queued %d packets before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}

	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
	session.worldMu.Lock()
	packets := append([]modernworld.Packet(nil), session.pendingInstance...)
	session.worldMu.Unlock()
	if packets[0].Opcode != modernworld.SMSGItemCooldown {
		t.Fatalf("first opcode=%d", packets[0].Opcode)
	}
	wantCooldown := modernworld.EncodeItemCooldown(modernItem, 2828, modernworld.DefaultItemCooldownMS)
	if !bytes.Equal(packets[0].Body, wantCooldown) {
		t.Fatalf("cooldown body=%x want=%x", packets[0].Body, wantCooldown)
	}
	if packets[1].Opcode != modernworld.SMSGDurabilityDamageDeath {
		t.Fatalf("second opcode=%d", packets[1].Opcode)
	}
	if !bytes.Equal(packets[1].Body, modernworld.EncodeDurabilityDamageDeath()) {
		t.Fatalf("death body=%x", packets[1].Body)
	}
}

func TestSocketGemsForwardsAndAcks(t *testing.T) {
	const (
		legacyItem = uint64(0x4000000000000681)
		legacyGem  = uint64(0x4000000000000099)
	)
	modernItem := modernworld.GUID128{Low: 0x681, High: 1}
	modernGem := modernworld.GUID128{Low: 0x99, High: 1}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyItem: modernItem, legacyGem: modernGem},
	}

	requestBody := modernworld.EncodeSocketGemsSuccess(modernItem)
	requestBody = append(requestBody, modernworld.EncodeSocketGemsSuccess(modernGem)...)
	requestBody = append(requestBody, modernworld.EncodeSocketGemsSuccess(modernworld.GUID128{})...)
	requestBody = append(requestBody, modernworld.EncodeSocketGemsSuccess(modernworld.GUID128{})...)

	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGSocketGems, Body: requestBody})
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	written := legacyConnection.nextWrite(t)
	if written.opcode != legacyworld.CMSGSocketGems {
		t.Fatalf("legacy opcode=%d", written.opcode)
	}
	want := modernworld.EncodeLegacySocketGems(legacyItem, [3]uint64{legacyGem, 0, 0})
	if !bytes.Equal(written.body, want) {
		t.Fatalf("legacy body=%x want=%x", written.body, want)
	}
	session.worldMu.Lock()
	defer session.worldMu.Unlock()
	if len(session.pendingInstance) != 1 || session.pendingInstance[0].Opcode != modernworld.SMSGSocketGemsSuccess {
		t.Fatalf("pending=%v", session.pendingInstance)
	}
	if !bytes.Equal(session.pendingInstance[0].Body, modernworld.EncodeSocketGemsSuccess(modernItem)) {
		t.Fatalf("success body=%x", session.pendingInstance[0].Body)
	}
}

func TestItemControlsForwardToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}

	requests := []struct {
		name   string
		packet modernworld.Packet
		opcode uint32
		body   []byte
	}{
		{
			name:   "auto store bag item",
			packet: modernworld.Packet{Opcode: modernworld.CMSGAutoStoreBagItem, Body: []byte{0x00, 0x04, 0x03, 0x05}},
			opcode: legacyworld.CMSGAutostoreBagItem,
			body:   []byte{0x03, 0x05, 0x04},
		},
		{
			name:   "split item",
			packet: modernworld.Packet{Opcode: modernworld.CMSGSplitItem, Body: []byte{0x00, 0x04, 0x05, 0x0B, 0x06, 7, 0, 0, 0}},
			opcode: legacyworld.CMSGSplitItem,
			body:   []byte{0x04, 0x05, 0x0B, 0x06, 7, 0, 0, 0},
		},
		{
			name:   "wrap item",
			packet: modernworld.Packet{Opcode: modernworld.CMSGWrapItem, Body: []byte{0x00, 0x02, 0x03, 0x04, 0x05}},
			opcode: legacyworld.CMSGWrapItem,
			body:   []byte{0x02, 0x03, 0x04, 0x05},
		},
		{
			name:   "cancel temp enchant",
			packet: modernworld.Packet{Opcode: modernworld.CMSGCancelTempEnchantment, Body: []byte{2, 0, 0, 0}},
			opcode: legacyworld.CMSGCancelTempEnchantment,
			body:   []byte{2, 0, 0, 0},
		},
	}
	for _, request := range requests {
		handled, err := server.handleModernPlayPacket(session, nil, request.packet)
		if err != nil || !handled {
			t.Fatalf("%s handled=%v err=%v", request.name, handled, err)
		}
		write := legacyConnection.nextWrite(t)
		if write.opcode != request.opcode || !bytes.Equal(write.body, request.body) {
			t.Fatalf("%s write=%#v, want opcode=0x%x body=%x", request.name, write, request.opcode, request.body)
		}
	}

	// Malformed bodies are rejected rather than forwarded.
	for _, tc := range []struct {
		name   string
		packet modernworld.Packet
	}{
		{name: "split zero qty", packet: modernworld.Packet{Opcode: modernworld.CMSGSplitItem, Body: []byte{0x00, 0x04, 0x05, 0x0B, 0x06, 0, 0, 0, 0}}},
		{name: "wrap truncated", packet: modernworld.Packet{Opcode: modernworld.CMSGWrapItem, Body: []byte{0x00, 0x02, 0x03, 0x04}}},
		{name: "cancel temp short", packet: modernworld.Packet{Opcode: modernworld.CMSGCancelTempEnchantment, Body: []byte{1, 2}}},
	} {
		if _, err := server.handleModernPlayPacket(session, nil, tc.packet); err == nil {
			t.Fatalf("%s: expected malformed-body error", tc.name)
		}
	}
}

func TestAddIgnoreForwardsToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}

	// 9-bit MSB-first length (3) then the name bytes.
	body := []byte{0x01, 0x80}
	body = append(body, "Eve"...)
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGAddIgnore, Body: body})
	if err != nil || !handled {
		t.Fatalf("add-ignore handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGAddIgnore || !bytes.Equal(write.body, []byte("Eve\x00")) {
		t.Fatalf("add-ignore write=%#v", write)
	}

	if _, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGAddIgnore, Body: []byte{0x01, 0x80}}); err == nil {
		t.Fatal("expected truncated add-ignore name to error")
	}
}

func TestDelFriendAndSetContactNotesForwardToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}

	delBody := binary.LittleEndian.AppendUint32(nil, 1)
	delBody = append(delBody, testModernPlayerGUID(0x43)...)
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGDelFriend, Body: delBody})
	if err != nil || !handled {
		t.Fatalf("del-friend handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGDelFriend || binary.LittleEndian.Uint64(write.body) != 0x43 {
		t.Fatalf("del-friend write=%#v", write)
	}

	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGDelIgnore, Body: delBody})
	if err != nil || !handled {
		t.Fatalf("del-ignore handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGDelIgnore || binary.LittleEndian.Uint64(write.body) != 0x43 {
		t.Fatalf("del-ignore write=%#v", write)
	}

	note := "healer"
	notesBody := binary.LittleEndian.AppendUint32(nil, 1)
	notesBody = append(notesBody, testModernPlayerGUID(0x43)...)
	// 10-bit length 6 is the same first-byte packing as 9-bit length 3.
	notesBody = append(notesBody, 0x01, 0x80)
	notesBody = append(notesBody, note...)
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGSetContactNotes, Body: notesBody})
	if err != nil || !handled {
		t.Fatalf("set-notes handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	want := binary.LittleEndian.AppendUint64(nil, 0x43)
	want = append(want, "healer"...)
	want = append(want, 0)
	if write.opcode != legacyworld.CMSGSetContactNotes || !bytes.Equal(write.body, want) {
		t.Fatalf("set-notes write=%#v want=%x", write, want)
	}
}

func TestTaxiRequestsForwardToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	modernGUID := modernworld.GUID128{Low: 0x43, High: uint64(2)<<58 | uint64(1)<<42}
	usable := make([]byte, 56)
	usable[0] = 0x0a // nodes 2 and 4
	session := &proxySession{
		legacy:          &legacyauth.Session{Username: "TEST"},
		legacyWorld:     legacyConnection,
		objectGUIDs:     map[uint64]modernworld.GUID128{0xf130000001000043: modernGUID},
		currentTaxiNode: 2,
		usableTaxiNodes: usable,
	}
	guidBody := testModernPlayerGUID(0x43)
	for _, request := range []struct {
		opcode uint16
		legacy uint32
	}{
		{modernworld.CMSGTaxiNodeStatusQuery, legacyworld.CMSGTaxiNodeStatusQuery},
		{modernworld.CMSGTaxiQueryAvailableNodes, legacyworld.CMSGTaxiQueryAvailableNodes},
		{modernworld.CMSGEnableTaxiNode, legacyworld.CMSGGossipHello},
	} {
		handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: request.opcode, Body: guidBody})
		if err != nil || !handled {
			t.Fatalf("taxi opcode=%d handled=%v err=%v", request.opcode, handled, err)
		}
		write := legacyConnection.nextWrite(t)
		if write.opcode != request.legacy || binary.LittleEndian.Uint64(write.body) != 0xf130000001000043 {
			t.Fatalf("taxi opcode=%d write=%#v", request.opcode, write)
		}
	}
	activate := append([]byte(nil), guidBody...)
	activate = binary.LittleEndian.AppendUint32(activate, 4)
	activate = binary.LittleEndian.AppendUint32(activate, 0)
	activate = binary.LittleEndian.AppendUint32(activate, 0)
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGActivateTaxi, Body: activate})
	if err != nil || !handled {
		t.Fatalf("activate taxi handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGActivateTaxi || len(write.body) != 16 || binary.LittleEndian.Uint32(write.body[8:]) != 2 || binary.LittleEndian.Uint32(write.body[12:]) != 4 {
		t.Fatalf("activate taxi write=%#v", write)
	}
}

func TestBinderActivateForwardsToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	modernGUID := modernworld.GUID128{Low: 0x43, High: uint64(2)<<58 | uint64(1)<<42}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{0xf130000001000043: modernGUID},
	}
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{
		Opcode: modernworld.CMSGBinderActivate,
		Body:   testModernPlayerGUID(0x43),
	})
	if err != nil || !handled {
		t.Fatalf("binder activate handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGBinderActivate || len(write.body) != 8 || binary.LittleEndian.Uint64(write.body) != 0xf130000001000043 {
		t.Fatalf("binder activate write=%#v", write)
	}
}

func TestSpiritHealerConfirmAutoActivatesLegacy(t *testing.T) {
	const healer = uint64(0xf13000195b001f7f)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	legacyConnection.reads <- legacyworld.Packet{
		Opcode: legacyworld.SMSGSpiritHealerConfirm,
		Body:   modernworld.EncodeLegacyUnpackedGUID(healer),
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGSpiritHealerActivate ||
		!bytes.Equal(write.body, modernworld.EncodeLegacyUnpackedGUID(healer)) {
		t.Fatalf("unexpected spirit-healer activation: %#v", write)
	}

	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
}

func TestLogoutCompleteResetPreservesCharacterListAndQueuedPacket(t *testing.T) {
	characters := map[uint64]struct{}{0x42: {}}
	session := &proxySession{
		knownCharacters:        characters,
		currentCharacter:       0x42,
		currentMapID:           571,
		activePlayerCreated:    true,
		knownSpells:            []uint32{1752},
		hasKnownSpells:         true,
		playerAuras:            []modernworld.AuraInfo{{SpellID: 1784}},
		pendingInstance:        []modernworld.Packet{{Opcode: modernworld.SMSGLogoutComplete}},
		objectGUIDs:            map[uint64]modernworld.GUID128{0x42: {Low: 0x42}},
		objectTypes:            map[uint64]uint8{0x42: 4},
		objectFields:           map[uint64]map[int]uint32{0x42: {0: 0x42}},
		waitingForNewWorld:     true,
		waitingForWorldPortAck: true,
	}

	session.resetCharacterSessionLocked()
	if session.currentCharacter != 0 || session.currentMapID != 0 || session.activePlayerCreated {
		t.Fatalf("character state was not reset: guid=%x map=%d active=%v", session.currentCharacter, session.currentMapID, session.activePlayerCreated)
	}
	if len(session.knownSpells) != 0 || session.hasKnownSpells || len(session.playerAuras) != 0 {
		t.Fatal("spell or aura state survived logout")
	}
	if len(session.objectGUIDs) != 0 || len(session.objectTypes) != 0 || len(session.objectFields) != 0 {
		t.Fatal("world-object state survived logout")
	}
	if session.knownCharacters == nil || len(session.knownCharacters) != 1 {
		t.Fatal("character list was cleared during logout")
	}
	if len(session.pendingInstance) != 1 || session.pendingInstance[0].Opcode != modernworld.SMSGLogoutComplete {
		t.Fatal("queued logout-complete packet was cleared during reset")
	}
}

func TestCompletedQuestStateSendsActivePlayerUpdates(t *testing.T) {
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		currentCharacter:    0x42,
		currentMapID:        571,
		activePlayerCreated: true,
		objectGUIDs:         make(map[uint64]modernworld.GUID128),
	}
	// Quest IDs 39, 98 and 99 map to QuestV2 UniqueBitFlag 1, 64 and 65.
	if err := server.replaceCompletedQuests(session, []uint32{39, 98, 99}); err != nil {
		t.Fatal(err)
	}
	if len(session.pendingInstance) != 1 || session.pendingInstance[0].Opcode != modernworld.SMSGUpdateObject {
		t.Fatalf("initial completed-quest packets = %#v", session.pendingInstance)
	}
	if got := session.completedQuestBlocks[0]; got != uint64(1)|uint64(1)<<63 {
		t.Fatalf("block 0 = 0x%x", got)
	}
	if got := session.completedQuestBlocks[1]; got != 1 {
		t.Fatalf("block 1 = 0x%x", got)
	}
	// Quest ID 2258 maps to UniqueBitFlag 66.
	if err := server.markQuestCompleted(session, 2258); err != nil {
		t.Fatal(err)
	}
	if len(session.pendingInstance) != 2 || session.completedQuestBlocks[1] != 3 {
		t.Fatalf("incremental completion packets=%d block1=0x%x", len(session.pendingInstance), session.completedQuestBlocks[1])
	}
	if err := server.markQuestCompleted(session, 2258); err != nil {
		t.Fatal(err)
	}
	if len(session.pendingInstance) != 2 {
		t.Fatalf("duplicate completion emitted %d packets", len(session.pendingInstance))
	}
}

func TestQuestItemProgressFlushUsesCurrentInventory(t *testing.T) {
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:               &legacyauth.Session{Username: "TEST"},
		currentCharacter:     1,
		questItemSyncPending: true,
		objectTypes:          map[uint64]uint8{1: 4, 2: 1},
		objectFields: map[uint64]map[int]uint32{
			1: {158: 39},
			2: {3: 1234, 14: 2},
		},
		questItemObjectives: map[uint32][]modernworld.QuestItemObjectiveInfo{
			39: {{QuestID: 39, ItemID: 1234, Required: 6}},
		},
	}
	if err := server.flushQuestItemProgress(session); err != nil {
		t.Fatal(err)
	}
	if session.questItemSyncPending || len(session.pendingInstance) != 1 {
		t.Fatalf("pending=%v packets=%d", session.questItemSyncPending, len(session.pendingInstance))
	}
	packet := session.pendingInstance[0]
	if packet.Opcode != modernworld.SMSGQuestUpdateAddCredit || len(packet.Body) < 15 {
		t.Fatalf("unexpected quest progress packet: %#v", packet)
	}
	// An empty packed GUID occupies the two leading mask bytes.
	if questID := binary.LittleEndian.Uint32(packet.Body[2:]); questID != 39 {
		t.Fatalf("quest=%d body=%x", questID, packet.Body)
	}
	if count := binary.LittleEndian.Uint16(packet.Body[10:]); count != 2 {
		t.Fatalf("count=%d body=%x", count, packet.Body)
	}
}

func TestServerStartsThreeEndpoints(t *testing.T) {
	config := DefaultConfig()
	config.CUFDataDir = t.TempDir()
	storedProfiles := modernworld.DefaultCUFProfiles()
	storedProfiles[0].Name = "已保存布局"
	storedProfiles[0].FrameWidth = 109
	wantCUFProfiles := modernworld.EncodeLoadCUFProfiles(storedProfiles)
	if err := (&Server{config: config}).saveCUFProfiles(cufTestSession("TESTER", 1), wantCUFProfiles); err != nil {
		t.Fatal(err)
	}
	config.BNetAddress = "127.0.0.1:0"
	config.RESTAddress = "127.0.0.1:0"
	config.WorldAddress = "127.0.0.1:0"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := New(config, logger)
	if err != nil {
		t.Fatal(err)
	}
	server.legacyLogin = func(_ context.Context, address string, credentials legacyauth.Credentials) (*legacyauth.Session, error) {
		if address != config.LegacyAuth || credentials.Username != "tester" || credentials.Password != "secret" || credentials.Locale != "zhCN" {
			t.Fatalf("unexpected legacy login: address=%q credentials=%#v", address, credentials)
		}
		return &legacyauth.Session{Username: "TESTER", Realms: []legacyauth.Realm{{ID: 1, Name: "AzerothCore"}}}, nil
	}
	type legacyWorldCall struct {
		realmID uint32
		account string
	}
	legacyWorldCalls := make(chan legacyWorldCall, 1)
	legacyConnection := newFakeLegacyWorldConnection()
	server.legacyWorld = func(_ context.Context, selected legacyauth.Realm, session *legacyauth.Session) (legacyWorldConnection, error) {
		legacyWorldCalls <- legacyWorldCall{realmID: selected.ID, account: session.Username}
		return legacyConnection, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()

	addresses := waitForAddresses(t, server)
	tlsConfig := &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12} // test-only development certificate
	conn, err := tls.Dial("tcp", addresses["bnet"], tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := bnet.WriteFrame(conn, bnet.Frame{
		Header:  bnet.Header{ServiceHash: serviceConnection, MethodID: 1, Token: 1},
		Payload: bnet.EncodeConnectRequest(bnet.ConnectRequest{UseBindlessRPC: true}),
	}); err != nil {
		t.Fatal(err)
	}
	connectResponse, err := bnet.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	assertRPCResponse(t, connectResponse, serviceConnection, 1, 1, rpcOK)
	if len(connectResponse.Payload) == 0 {
		t.Fatal("connect response payload is empty")
	}

	if err := bnet.WriteFrame(conn, bnet.Frame{
		Header: bnet.Header{ServiceHash: serviceAuthentication, MethodID: 1, Token: 2},
		Payload: bnet.EncodeLogonRequest(bnet.LogonRequest{
			Program:            "WoW",
			Platform:           "Win",
			Locale:             "zhCN",
			ApplicationVersion: 54261,
		}),
	}); err != nil {
		t.Fatal(err)
	}
	challengeFrame, err := bnet.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if challengeFrame.Header.ServiceHash != listenerChallenge || challengeFrame.Header.MethodID != 3 {
		t.Fatalf("unexpected challenge header: %#v", challengeFrame.Header)
	}
	if header := bnet.MarshalHeader(challengeFrame.Header); len(header) < 2 || header[0] != 0x08 || header[1] != 0x00 {
		t.Fatalf("challenge header missing required service_id=0: %x", bnet.MarshalHeader(challengeFrame.Header))
	}
	challenge, err := bnet.DecodeExternalChallenge(challengeFrame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	wantLoginURL := "https://" + addresses["rest"] + "/bnetserver/login/Win/54261/zhCN/"
	if challenge.PayloadType != "web_auth_url" || challenge.URL != wantLoginURL {
		t.Fatalf("unexpected challenge: %#v, want URL %q", challenge, wantLoginURL)
	}
	logonResponse, err := bnet.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	assertRPCResponse(t, logonResponse, serviceAuthentication, 1, 2, rpcOK)

	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	response, err := client.Get("https://" + addresses["rest"] + "/bnetserver/login/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login form status %d", response.StatusCode)
	}
	var form struct {
		Type   string `json:"type"`
		Inputs []struct {
			InputID string `json:"input_id"`
		} `json:"inputs"`
	}
	if err := json.NewDecoder(response.Body).Decode(&form); err != nil {
		t.Fatal(err)
	}
	if form.Type != "LOGIN_FORM" || len(form.Inputs) != 3 || form.Inputs[0].InputID != "account_name" {
		t.Fatalf("unexpected login form: %#v", form)
	}
	loginBody := `{"version":"1","program_id":"WoW","platform_id":"Win_x64","inputs":[{"input_id":"account_name","value":"tester"},{"input_id":"password","value":"secret"}]}`
	response, err = client.Post(
		"https://"+addresses["rest"]+"/bnetserver/login/Win/54261/zhCN/",
		"application/json",
		strings.NewReader(loginBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("login status %d: %s", response.StatusCode, body)
	}
	var loginResult struct {
		State  string `json:"authentication_state"`
		Ticket string `json:"login_ticket"`
	}
	if err := json.NewDecoder(response.Body).Decode(&loginResult); err != nil {
		t.Fatal(err)
	}
	if loginResult.State != "DONE" || !strings.HasPrefix(loginResult.Ticket, "RS-") {
		t.Fatalf("unexpected login result: %#v", loginResult)
	}

	if err := bnet.WriteFrame(conn, bnet.Frame{
		Header:  bnet.Header{ServiceHash: serviceAuthentication, MethodID: 7, Token: 3},
		Payload: bnet.EncodeWebCredentials(loginResult.Ticket),
	}); err != nil {
		t.Fatal(err)
	}
	resultFrame, err := bnet.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if resultFrame.Header.ServiceHash != listenerAuthentication || resultFrame.Header.MethodID != 5 {
		t.Fatalf("unexpected logon result header: %#v", resultFrame.Header)
	}
	logonResult, err := bnet.DecodeLogonResult(resultFrame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if logonResult.ErrorCode != 0 || len(logonResult.SessionKey) != 64 {
		t.Fatalf("unexpected logon result: error=%d key_bytes=%d", logonResult.ErrorCode, len(logonResult.SessionKey))
	}
	verifyResponse, err := bnet.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	assertRPCResponse(t, verifyResponse, serviceAuthentication, 7, 3, rpcOK)
	for _, accountRPC := range []struct {
		method uint32
		token  uint32
	}{
		{method: 30, token: 30},
		{method: 31, token: 31},
	} {
		if err := bnet.WriteFrame(conn, bnet.Frame{Header: bnet.Header{
			ServiceHash: serviceAccount,
			MethodID:    accountRPC.method,
			Token:       accountRPC.token,
		}}); err != nil {
			t.Fatal(err)
		}
		accountFrame, err := bnet.ReadFrame(conn)
		if err != nil {
			t.Fatal(err)
		}
		assertRPCResponse(t, accountFrame, serviceAccount, accountRPC.method, accountRPC.token, rpcOK)
		if len(accountFrame.Payload) == 0 {
			t.Fatalf("account method %d returned an empty payload", accountRPC.method)
		}
	}

	const realmListCommand = "Command_RealmListRequest_v1_wotlk1"
	if err := bnet.WriteFrame(conn, bnet.Frame{
		Header:  bnet.Header{ServiceHash: serviceGameUtilities, MethodID: 10, Token: 4},
		Payload: bnet.EncodeGetAllValuesRequest(realmListCommand),
	}); err != nil {
		t.Fatal(err)
	}
	subRegionFrame, err := bnet.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	assertRPCResponse(t, subRegionFrame, serviceGameUtilities, 10, 4, rpcOK)
	subRegions, err := bnet.DecodeGetAllValuesResponse(subRegionFrame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(subRegions) != 1 || subRegions[0].StringValue == nil || *subRegions[0].StringValue != realm.SubRegion {
		t.Fatalf("unexpected subregions: %#v", subRegions)
	}

	if err := bnet.WriteFrame(conn, bnet.Frame{
		Header: bnet.Header{ServiceHash: serviceGameUtilities, MethodID: 1, Token: 5},
		Payload: bnet.EncodeClientRequest([]bnet.Attribute{
			{
				Name:  "Command_RealmListTicketRequest_v1_wotlk1",
				Value: bnet.StringVariant(""),
			},
			{
				Name:  "Param_ClientInfo",
				Value: bnet.BlobVariant([]byte(`JSONRealmListTicketClientInformation:{"info":{"secret":[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31]}}`)),
			},
		}),
	}); err != nil {
		t.Fatal(err)
	}
	ticketFrame, err := bnet.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	assertRPCResponse(t, ticketFrame, serviceGameUtilities, 1, 5, rpcOK)
	ticketAttributes, err := bnet.DecodeClientResponse(ticketFrame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	realmTicket, ok := bnet.FindAttribute(ticketAttributes, "Param_RealmListTicket")
	if !ok || !realmTicket.HasBlob || string(realmTicket.BlobValue) != "AuthRealmListTicket" {
		t.Fatalf("unexpected realm-list ticket attributes: %#v", ticketAttributes)
	}

	if err := bnet.WriteFrame(conn, bnet.Frame{
		Header: bnet.Header{ServiceHash: serviceGameUtilities, MethodID: 1, Token: 6},
		Payload: bnet.EncodeClientRequest([]bnet.Attribute{{
			Name:  realmListCommand,
			Value: bnet.StringVariant(realm.SubRegion),
		}}),
	}); err != nil {
		t.Fatal(err)
	}
	realmListFrame, err := bnet.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	assertRPCResponse(t, realmListFrame, serviceGameUtilities, 1, 6, rpcOK)
	realmAttributes, err := bnet.DecodeClientResponse(realmListFrame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	listVariant, ok := bnet.FindAttribute(realmAttributes, "Param_RealmList")
	if !ok || !listVariant.HasBlob {
		t.Fatalf("missing realm list: %#v", realmAttributes)
	}
	listType, listJSON, err := realm.InflateJSON(listVariant.BlobValue)
	if err != nil {
		t.Fatal(err)
	}
	if listType != "JSONRealmListUpdates" || !strings.Contains(string(listJSON), "AzerothCore") {
		t.Fatalf("unexpected realm list type=%q JSON=%s", listType, listJSON)
	}
	countVariant, ok := bnet.FindAttribute(realmAttributes, "Param_CharacterCountList")
	if !ok || !countVariant.HasBlob {
		t.Fatalf("missing character counts: %#v", realmAttributes)
	}
	countType, _, err := realm.InflateJSON(countVariant.BlobValue)
	if err != nil || countType != "JSONRealmCharacterCountList" {
		t.Fatalf("character count type=%q err=%v", countType, err)
	}

	if err := bnet.WriteFrame(conn, bnet.Frame{
		Header: bnet.Header{ServiceHash: serviceGameUtilities, MethodID: 1, Token: 7},
		Payload: bnet.EncodeClientRequest([]bnet.Attribute{
			{Name: "Command_RealmJoinRequest_v1_wotlk1", Value: bnet.StringVariant("")},
			{Name: "Param_RealmAddress", Value: bnet.UintVariant(uint64(realm.Address(1)))},
		}),
	}); err != nil {
		t.Fatal(err)
	}
	joinFrame, err := bnet.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	assertRPCResponse(t, joinFrame, serviceGameUtilities, 1, 7, rpcOK)
	joinAttributes, err := bnet.DecodeClientResponse(joinFrame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	joinTicket, ok := bnet.FindAttribute(joinAttributes, "Param_RealmJoinTicket")
	if !ok || !joinTicket.HasBlob || string(joinTicket.BlobValue) != "TESTER" {
		t.Fatalf("unexpected join ticket: %#v", joinAttributes)
	}
	joinSecret, ok := bnet.FindAttribute(joinAttributes, "Param_JoinSecret")
	if !ok || !joinSecret.HasBlob || len(joinSecret.BlobValue) != 32 {
		t.Fatalf("unexpected join secret: %#v", joinAttributes)
	}
	serverAddress, ok := bnet.FindAttribute(joinAttributes, "Param_ServerAddresses")
	if !ok || !serverAddress.HasBlob {
		t.Fatalf("missing server address: %#v", joinAttributes)
	}
	addressType, addressJSON, err := realm.InflateJSON(serverAddress.BlobValue)
	if err != nil || addressType != "JSONRealmListServerIPAddresses" || !strings.Contains(string(addressJSON), addresses["world"][:strings.LastIndex(addresses["world"], ":")]) {
		t.Fatalf("server address type=%q JSON=%s err=%v", addressType, addressJSON, err)
	}
	server.sessionsMu.RLock()
	worldSession := server.worldSessions["TESTER"]
	server.sessionsMu.RUnlock()
	if worldSession == nil || !worldSession.hasRealm || worldSession.worldKey[0] != 0 || worldSession.worldKey[31] != 31 {
		t.Fatalf("world session was not prepared: %#v", worldSession)
	}

	// The real client disconnects BNet after realm join. It must receive the
	// notification and EOF, while the world session remains usable below.
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := bnet.WriteFrame(conn, bnet.Frame{
		Header:  bnet.Header{ServiceHash: serviceConnection, MethodID: 7, Token: 10},
		Payload: []byte{0x08, 0x00},
	}); err != nil {
		t.Fatal(err)
	}
	disconnect, err := bnet.ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if disconnect.Header.IsResponse || disconnect.Header.ServiceHash != serviceConnection || disconnect.Header.MethodID != 4 || !bytes.Equal(disconnect.Payload, []byte{0x08, 0x00}) {
		t.Fatalf("expected ForceDisconnect notification, got %#v", disconnect)
	}
	if _, err := bnet.ReadFrame(conn); !errors.Is(err, io.EOF) {
		t.Fatalf("expected BNet EOF after notification, got %v", err)
	}

	// Returning to the login screen can start another challenge on a fresh
	// front connection without restarting the proxy or the client process.
	reconnected, err := tls.Dial("tcp", addresses["bnet"], tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	_ = reconnected.SetDeadline(time.Now().Add(3 * time.Second))
	for _, request := range []bnet.Frame{
		{Header: bnet.Header{ServiceHash: serviceConnection, MethodID: 1}, Payload: bnet.EncodeConnectRequest(bnet.ConnectRequest{UseBindlessRPC: true})},
		{Header: bnet.Header{ServiceHash: serviceAuthentication, MethodID: 1, Token: 1}, Payload: bnet.EncodeLogonRequest(bnet.LogonRequest{Program: "WoW", Platform: "Win", Locale: "zhCN", ApplicationVersion: 54261})},
	} {
		if err := bnet.WriteFrame(reconnected, request); err != nil {
			t.Fatal(err)
		}
		response, err := bnet.ReadFrame(reconnected)
		if err != nil {
			t.Fatal(err)
		}
		if request.Header.ServiceHash == serviceConnection {
			assertRPCResponse(t, response, serviceConnection, 1, 0, rpcOK)
		} else if response.Header.ServiceHash != listenerChallenge || response.Header.MethodID != 3 {
			t.Fatalf("reconnect did not receive login challenge: %#v", response)
		}
	}
	if _, err := bnet.ReadFrame(reconnected); err != nil {
		t.Fatal(err)
	}
	reconnected.Close()

	worldRaw, err := net.DialTimeout("tcp", addresses["world"], 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer worldRaw.Close()
	_ = worldRaw.SetDeadline(time.Now().Add(3 * time.Second))
	serverInitializer := make([]byte, len(modernworld.ServerInitializer))
	if _, err := io.ReadFull(worldRaw, serverInitializer); err != nil {
		t.Fatal(err)
	}
	if string(serverInitializer) != modernworld.ServerInitializer {
		t.Fatalf("unexpected world initializer: %q", serverInitializer)
	}
	if _, err := worldRaw.Write([]byte(modernworld.ClientInitializer)); err != nil {
		t.Fatal(err)
	}
	worldClient := modernworld.NewClientConn(worldRaw)
	challengePacket, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if challengePacket.Opcode != modernworld.SMSGAuthChallenge || len(challengePacket.Body) != 49 {
		t.Fatalf("unexpected modern world challenge: %#v", challengePacket)
	}
	var serverChallenge [16]byte
	copy(serverChallenge[:], challengePacket.Body[32:48])
	authSession := modernworld.AuthSession{
		RegionID:        1,
		BattlegroupID:   1,
		RealmID:         1,
		RealmJoinTicket: "TESTER",
	}
	for index := range authSession.LocalChallenge {
		authSession.LocalChallenge[index] = byte(0x80 + index)
	}
	if err := worldClient.WritePacket(modernworld.CMSGAuthSession, modernworld.EncodeAuthSession(authSession)); err != nil {
		t.Fatal(err)
	}
	_, encryptionKey := modernworld.DeriveKeys(worldSession.worldKey, serverChallenge, authSession.LocalChallenge)
	enterPacket, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if enterPacket.Opcode != modernworld.SMSGEnterEncryptedMode {
		t.Fatalf("unexpected enter-encrypted-mode opcode: %d", enterPacket.Opcode)
	}
	if err := modernworld.VerifyEnterEncryptedMode(enterPacket.Body, encryptionKey); err != nil {
		t.Fatal(err)
	}
	if err := worldClient.WritePacket(modernworld.CMSGEnterEncryptedModeAck, nil); err != nil {
		t.Fatal(err)
	}
	if err := worldClient.EnableEncryption(encryptionKey[:]); err != nil {
		t.Fatal(err)
	}
	modernAuthResponse, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if modernAuthResponse.Opcode != modernworld.SMSGAuthResponse || len(modernAuthResponse.Body) == 0 {
		t.Fatalf("unexpected modern auth response: %#v", modernAuthResponse)
	}
	expectedGlue := []uint16{
		modernworld.SMSGSetTimeZoneInformation,
		modernworld.SMSGFeatureSystemStatusGlue,
		modernworld.SMSGCacheVersion,
		modernworld.SMSGAvailableHotfixes,
		modernworld.SMSGBattleNetConnectionStatus,
	}
	for _, expectedOpcode := range expectedGlue {
		glue, err := worldClient.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if glue.Opcode != expectedOpcode {
			t.Fatalf("world glue opcode = %d, want %d", glue.Opcode, expectedOpcode)
		}
	}
	hotfixRequest := binary.LittleEndian.AppendUint32(nil, 54261)
	hotfixRequest = binary.LittleEndian.AppendUint32(hotfixRequest, 0)
	hotfixRequest = binary.LittleEndian.AppendUint32(hotfixRequest, 1)
	hotfixRequest = binary.LittleEndian.AppendUint32(hotfixRequest, 3_000_001)
	if err := worldClient.WritePacket(modernworld.CMSGHotfixRequest, hotfixRequest); err != nil {
		t.Fatal(err)
	}
	hotfixResponse, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if hotfixResponse.Opcode != modernworld.SMSGHotfixConnect || len(hotfixResponse.Body) < 29 || binary.LittleEndian.Uint32(hotfixResponse.Body[:4]) != 1 {
		t.Fatalf("unexpected hotfix response: opcode=%d body=%x", hotfixResponse.Opcode, hotfixResponse.Body)
	}
	dbQuery := binary.LittleEndian.AppendUint32(nil, 0xDF2F53CF)
	dbQuery = append(dbQuery, 0, 8) // one 13-bit MSB-first record count
	dbQuery = binary.LittleEndian.AppendUint32(dbQuery, 123)
	if err := worldClient.WritePacket(modernworld.CMSGDBQueryBulk, dbQuery); err != nil {
		t.Fatal(err)
	}
	dbReply, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if dbReply.Opcode != modernworld.SMSGDBReply || len(dbReply.Body) != 17 || dbReply.Body[12] != 0x80 {
		t.Fatalf("unexpected DB reply: opcode=%d body=%x", dbReply.Opcode, dbReply.Body)
	}
	if err := worldClient.WritePacket(modernworld.CMSGEnumCharacters, nil); err != nil {
		t.Fatal(err)
	}
	if write := legacyConnection.nextWrite(t); write.opcode != legacyworld.CMSGEnumCharacters || len(write.body) != 0 {
		t.Fatalf("unexpected legacy character-enum request: %#v", write)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGEnumCharactersResult, Body: testLegacyCharacterEnum("Rabbit", 0x42, false)}
	characterEnum, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if characterEnum.Opcode != modernworld.SMSGEnumCharactersResult || binary.LittleEndian.Uint32(characterEnum.Body[1:5]) != 1 {
		t.Fatalf("unexpected modern character enum: opcode=%d bytes=%d", characterEnum.Opcode, len(characterEnum.Body))
	}
	playerLoginBody := append(testModernPlayerGUID(0x42), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(playerLoginBody[len(playerLoginBody)-4:], math.Float32bits(777.0))
	if err := worldClient.WritePacket(modernworld.CMSGPlayerLogin, playerLoginBody); err != nil {
		t.Fatal(err)
	}
	connectTo, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if connectTo.Opcode != modernworld.SMSGConnectTo {
		t.Fatalf("unexpected connect-to opcode: %d", connectTo.Opcode)
	}
	if err := modernworld.VerifyConnectTo(connectTo.Body); err != nil {
		t.Fatal(err)
	}
	connectInfo, err := modernworld.ParseConnectTo(connectTo.Body)
	if err != nil {
		t.Fatal(err)
	}
	_, expectedInstancePortText, _ := net.SplitHostPort(addresses["world"])
	expectedInstancePort, _ := strconv.Atoi(expectedInstancePortText)
	if connectInfo.Serial != modernworld.ConnectToWorldAttempt1 || connectInfo.Connection != modernworld.ConnectionTypeInstance || connectInfo.Port != uint16(expectedInstancePort) || !connectInfo.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("unexpected connect-to endpoint: %#v", connectInfo)
	}
	loginWrite := legacyConnection.nextWrite(t)
	if loginWrite.opcode != legacyworld.CMSGPlayerLogin || len(loginWrite.body) != 8 || binary.LittleEndian.Uint64(loginWrite.body) != 0x42 {
		t.Fatalf("unexpected legacy player-login request: %#v", loginWrite)
	}
	legacyVerify := binary.LittleEndian.AppendUint32(nil, 571)
	for _, value := range []float32{1.25, 2.5, 3.75, 4.5} {
		legacyVerify = binary.LittleEndian.AppendUint32(legacyVerify, math.Float32bits(value))
	}
	// Exercise the real race: AzerothCore can answer before the client's second
	// world socket is ready, so this packet must be queued for that socket.
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGLoginVerifyWorld, Body: legacyVerify}
	completedQuestWrite := legacyConnection.nextWrite(t)
	if completedQuestWrite.opcode != legacyworld.CMSGQueryQuestsCompleted || len(completedQuestWrite.body) != 0 {
		t.Fatalf("unexpected login completed-quest query: %#v", completedQuestWrite)
	}
	legacyCompletedQuests := binary.LittleEndian.AppendUint32(nil, 3)
	legacyCompletedQuests = binary.LittleEndian.AppendUint32(legacyCompletedQuests, 39)
	legacyCompletedQuests = binary.LittleEndian.AppendUint32(legacyCompletedQuests, 98)
	legacyCompletedQuests = binary.LittleEndian.AppendUint32(legacyCompletedQuests, 99)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGQueryQuestsCompletedResponse, Body: legacyCompletedQuests}

	instanceRaw, err := net.DialTimeout("tcp", addresses["world"], 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer instanceRaw.Close()
	_ = instanceRaw.SetDeadline(time.Now().Add(3 * time.Second))
	serverInitializer = make([]byte, len(modernworld.ServerInitializer))
	if _, err := io.ReadFull(instanceRaw, serverInitializer); err != nil {
		t.Fatal(err)
	}
	if string(serverInitializer) != modernworld.ServerInitializer {
		t.Fatalf("unexpected instance initializer: %q", serverInitializer)
	}
	if _, err := instanceRaw.Write([]byte(modernworld.ClientInitializer)); err != nil {
		t.Fatal(err)
	}
	instanceClient := modernworld.NewClientConn(instanceRaw)
	instanceChallengePacket, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if instanceChallengePacket.Opcode != modernworld.SMSGAuthChallenge || len(instanceChallengePacket.Body) != 49 {
		t.Fatalf("unexpected instance challenge: %#v", instanceChallengePacket)
	}
	var instanceServerChallenge [16]byte
	copy(instanceServerChallenge[:], instanceChallengePacket.Body[32:48])
	continued := modernworld.AuthContinuedSession{Key: connectInfo.Key}
	for index := range continued.LocalChallenge {
		continued.LocalChallenge[index] = byte(0xc0 + index)
	}
	continued.Digest, _ = modernworld.DeriveContinuedKeys(worldSession.modernSessionKey, continued.Key, instanceServerChallenge, continued.LocalChallenge)
	if err := instanceClient.WritePacket(modernworld.CMSGAuthContinuedSession, modernworld.EncodeAuthContinuedSession(continued)); err != nil {
		t.Fatal(err)
	}
	instanceEnter, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	_, instanceEncryptionKey := modernworld.DeriveContinuedKeys(worldSession.modernSessionKey, continued.Key, instanceServerChallenge, continued.LocalChallenge)
	if instanceEnter.Opcode != modernworld.SMSGEnterEncryptedMode {
		t.Fatalf("unexpected instance enter-encrypted opcode: %d", instanceEnter.Opcode)
	}
	if err := modernworld.VerifyEnterEncryptedMode(instanceEnter.Body, instanceEncryptionKey); err != nil {
		t.Fatal(err)
	}
	if err := instanceClient.WritePacket(modernworld.CMSGEnterEncryptedModeAck, nil); err != nil {
		t.Fatal(err)
	}
	if err := instanceClient.EnableEncryption(instanceEncryptionKey[:]); err != nil {
		t.Fatal(err)
	}
	resume, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if resume.Opcode != modernworld.SMSGResumeComms || len(resume.Body) != 0 {
		t.Fatalf("unexpected resume-comms packet: %#v", resume)
	}
	loginVerify, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if loginVerify.Opcode != modernworld.SMSGLoginVerifyWorld || len(loginVerify.Body) != 24 || !bytes.Equal(loginVerify.Body[:20], legacyVerify) || binary.LittleEndian.Uint32(loginVerify.Body[20:]) != 0 {
		t.Fatalf("unexpected modern login-verify-world: opcode=%d body=%x", loginVerify.Opcode, loginVerify.Body)
	}
	cufProfiles, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if cufProfiles.Opcode != modernworld.SMSGLoadCUFProfiles || !bytes.Equal(cufProfiles.Body, wantCUFProfiles) {
		t.Fatalf("unexpected CUF profiles: opcode=%d body=%x", cufProfiles.Opcode, cufProfiles.Body)
	}
	worldSession.worldMu.Lock()
	completedBlock0 := worldSession.completedQuestBlocks[0]
	completedBlock1 := worldSession.completedQuestBlocks[1]
	syncPending := worldSession.completedQuestSyncPending
	worldSession.worldMu.Unlock()
	if completedBlock0 != uint64(1)|uint64(1)<<63 || completedBlock1 != 1 || syncPending {
		t.Fatalf("completed quests were not ready before login packets: block0=0x%x block1=0x%x pending=%v", completedBlock0, completedBlock1, syncPending)
	}
	legacyWorldStates := binary.LittleEndian.AppendUint32(nil, 571)
	legacyWorldStates = binary.LittleEndian.AppendUint32(legacyWorldStates, 12)
	legacyWorldStates = binary.LittleEndian.AppendUint32(legacyWorldStates, 34)
	legacyWorldStates = binary.LittleEndian.AppendUint16(legacyWorldStates, 1)
	legacyWorldStates = binary.LittleEndian.AppendUint32(legacyWorldStates, 100)
	legacyWorldStates = binary.LittleEndian.AppendUint32(legacyWorldStates, 7)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGInitWorldStates, Body: legacyWorldStates}
	earlyWorldStates, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if earlyWorldStates.Opcode != modernworld.SMSGInitWorldStates || binary.LittleEndian.Uint32(earlyWorldStates.Body) != 571 || binary.LittleEndian.Uint32(earlyWorldStates.Body[4:]) != 12 || binary.LittleEndian.Uint32(earlyWorldStates.Body[8:]) != 34 {
		t.Fatalf("unexpected early init-world-states: opcode=%d body=%x", earlyWorldStates.Opcode, earlyWorldStates.Body)
	}
	legacyActions := make([]byte, 1+144*4)
	binary.LittleEndian.PutUint32(legacyActions[1:], 0x12034567)
	legacyActions[0] = 1
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGUpdateActionButtons, Body: legacyActions}
	legacyKnownSpells := []byte{0}
	legacyKnownSpells = binary.LittleEndian.AppendUint16(legacyKnownSpells, 2)
	legacyKnownSpells = binary.LittleEndian.AppendUint32(legacyKnownSpells, 133)
	legacyKnownSpells = binary.LittleEndian.AppendUint16(legacyKnownSpells, 0)
	legacyKnownSpells = binary.LittleEndian.AppendUint32(legacyKnownSpells, 168)
	legacyKnownSpells = binary.LittleEndian.AppendUint16(legacyKnownSpells, 0)
	legacyKnownSpells = binary.LittleEndian.AppendUint16(legacyKnownSpells, 0)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGSendKnownSpells, Body: legacyKnownSpells}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGUpdateObject, Body: testLegacyActivePlayerCreate(0x42)}
	actionButtons, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if actionButtons.Opcode != modernworld.SMSGUpdateActionButtons || len(actionButtons.Body) != 1441 || actionButtons.Body[len(actionButtons.Body)-1] != 1 || binary.LittleEndian.Uint64(actionButtons.Body) != 0x12034567 {
		t.Fatalf("unexpected pre-create action buttons: opcode=%d bytes=%d reason=%d first=0x%x", actionButtons.Opcode, len(actionButtons.Body), actionButtons.Body[len(actionButtons.Body)-1], binary.LittleEndian.Uint64(actionButtons.Body))
	}
	knownSpells, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if knownSpells.Opcode != modernworld.SMSGSendKnownSpells || len(knownSpells.Body) != 17 || knownSpells.Body[0] != 0 || binary.LittleEndian.Uint32(knownSpells.Body[1:5]) != 2 || binary.LittleEndian.Uint32(knownSpells.Body[9:13]) != 133 || binary.LittleEndian.Uint32(knownSpells.Body[13:17]) != 168 {
		t.Fatalf("unexpected modern known spells: opcode=%d body=%x", knownSpells.Opcode, knownSpells.Body)
	}
	activePlayerCreate, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if activePlayerCreate.Opcode != modernworld.SMSGUpdateObject || binary.LittleEndian.Uint32(activePlayerCreate.Body) != 1 || binary.LittleEndian.Uint16(activePlayerCreate.Body[4:]) != 571 {
		t.Fatalf("unexpected active-player create: opcode=%d bytes=%d head=%x", activePlayerCreate.Opcode, len(activePlayerCreate.Body), activePlayerCreate.Body[:min(16, len(activePlayerCreate.Body))])
	}
	emptyUpdate, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if emptyUpdate.Opcode != modernworld.SMSGUpdateObject || len(emptyUpdate.Body) != 11 || binary.LittleEndian.Uint32(emptyUpdate.Body) != 0 {
		t.Fatalf("unexpected empty update marker: opcode=%d body=%x", emptyUpdate.Opcode, emptyUpdate.Body)
	}
	phaseShift, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if phaseShift.Opcode != modernworld.SMSGPhaseShiftChange {
		t.Fatalf("unexpected phase-shift packet: %#v", phaseShift)
	}
	worldStatesAgain, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if worldStatesAgain.Opcode != modernworld.SMSGInitWorldStates || !bytes.Equal(worldStatesAgain.Body, earlyWorldStates.Body) {
		t.Fatalf("post-create world states did not match cached packet")
	}
	postCreateSpells, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if postCreateSpells.Opcode != modernworld.SMSGSendKnownSpells || len(postCreateSpells.Body) != 17 || postCreateSpells.Body[0] != 0x80 || binary.LittleEndian.Uint32(postCreateSpells.Body[1:5]) != 2 || binary.LittleEndian.Uint32(postCreateSpells.Body[9:13]) != 133 || binary.LittleEndian.Uint32(postCreateSpells.Body[13:17]) != 168 {
		t.Fatalf("post-create known spells were not resent after ActivePlayer: opcode=%d body=%x", postCreateSpells.Opcode, postCreateSpells.Body)
	}
	localName, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if localName.Opcode != modernworld.SMSGQueryPlayerNames || !bytes.Contains(localName.Body, []byte("Rabbit")) {
		t.Fatalf("local player name was not seeded after ActivePlayer: opcode=%d body=%x", localName.Opcode, localName.Body)
	}
	legacyGhost := modernworld.EncodeLegacyPackedGUID(0x42)
	legacyGhost = append(legacyGhost, 0)
	legacyGhost = binary.LittleEndian.AppendUint32(legacyGhost, 8326)
	legacyGhost = append(legacyGhost, 0x01|0x08|0x10, 1, 1)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGAuraUpdateAll, Body: legacyGhost}
	auraUpdate, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if auraUpdate.Opcode != modernworld.SMSGAuraUpdate || auraUpdate.Body[0] != 0x80 || auraUpdate.Body[1]&0x40 == 0 || auraUpdate.Body[2] != 0 || auraUpdate.Body[3]&0x80 == 0 {
		t.Fatalf("unexpected post-create ghost aura: opcode=%d body=%x", auraUpdate.Opcode, auraUpdate.Body)
	}
	auraCastGUIDSize := testModernPackedGUIDSize(t, auraUpdate.Body[4:])
	auraVisualAt := 4 + auraCastGUIDSize + 4
	if got := binary.LittleEndian.Uint32(auraUpdate.Body[auraVisualAt:]); got != 241720 {
		t.Fatalf("post-create ghost aura visual=%d, want 241720", got)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGUpdateObject, Body: testLegacyPublicCreate(0xf130000001000043, 3)}
	unitCreate, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if unitCreate.Opcode != modernworld.SMSGUpdateObject || testModernCreatedObjectType(t, unitCreate.Body) != 5 {
		t.Fatalf("unexpected modern Unit create: opcode=%d bytes=%d", unitCreate.Opcode, len(unitCreate.Body))
	}
	creatureQuery := legacyConnection.nextWrite(t)
	if creatureQuery.opcode != legacyworld.CMSGCreatureQuery || binary.LittleEndian.Uint32(creatureQuery.body) != 1 {
		t.Fatalf("unexpected creature template query: %#v", creatureQuery)
	}
	// A 3.3.5 server broadcasts SMSG_AURA_UPDATE targeted at a creature when a
	// player-visible debuff such as Hunter's Mark (1130) lands on it. legacy proxy
	// forwards that to the modern client so the mark shows on the creature's
	// target frame; verify the proxy does the same and does not restrict the
	// forward to the local player.
	markAura := modernworld.EncodeLegacyPackedGUID(0xf130000001000043)
	markAura = append(markAura, 0) // aura slot
	markAura = binary.LittleEndian.AppendUint32(markAura, 1130)
	markAura = append(markAura, 0x80|0x01, 1, 1) // negative debuff with effect 0
	markAura = append(markAura, modernworld.EncodeLegacyPackedGUID(0x42)...)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGAuraUpdate, Body: markAura}
	creatureAura, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if creatureAura.Opcode != modernworld.SMSGAuraUpdate {
		t.Fatalf("creature aura update was not forwarded: opcode=%d body=%x", creatureAura.Opcode, creatureAura.Body)
	}
	if !bytes.Contains(creatureAura.Body, binary.LittleEndian.AppendUint32(nil, 1130)) {
		t.Fatalf("forwarded creature aura is missing Hunter's Mark spell 1130: body=%x", creatureAura.Body)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGOnMonsterMove, Body: testLegacyMonsterMove(0xf130000001000043)}
	monsterMove, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	guidSize := testModernPackedGUIDSize(t, monsterMove.Body)
	if monsterMove.Opcode != modernworld.SMSGOnMonsterMove || len(monsterMove.Body) < 70 || binary.LittleEndian.Uint32(monsterMove.Body[guidSize+37:]) != 1000 {
		t.Fatalf("unexpected modern monster move: opcode=%d bytes=%d body=%x", monsterMove.Opcode, len(monsterMove.Body), monsterMove.Body)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGOnMonsterMove, Body: testLegacyMonsterMove(0xf130000001000043)}
	returnMove, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if returnMove.Opcode != modernworld.SMSGOnMonsterMove {
		t.Fatalf("immediate monster return move was not forwarded: %#v", returnMove)
	}
	legacyLandWalk := modernworld.EncodeLegacyPackedGUID(0x42)
	legacyLandWalk = binary.LittleEndian.AppendUint32(legacyLandWalk, 17)
	legacyConnection.reads <- legacyworld.Packet{Opcode: 0x00df, Body: legacyLandWalk}
	landWalk, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if landWalk.Opcode != modernworld.SMSGMoveSetLandWalk || binary.LittleEndian.Uint32(landWalk.Body[len(landWalk.Body)-4:]) != 17 {
		t.Fatalf("unexpected modern land-walk packet: %#v", landWalk)
	}
	playerModernGUID := modernworld.ModernGUIDForLegacy(0x42, 571)
	flagAck := modernworld.EncodeMoveUpdate(modernworld.LegacyMovement{MoveTime: 200, X: 1, Y: 2, Z: 3, TransportSeat: -1}, playerModernGUID, modernworld.GUID128{})
	flagAck = binary.LittleEndian.AppendUint32(flagAck, 17)
	if err := instanceClient.WritePacket(modernworld.CMSGMoveWaterWalkAck, flagAck); err != nil {
		t.Fatal(err)
	}
	legacyFlagAck := legacyConnection.nextWrite(t)
	if legacyFlagAck.opcode != 0x02d0 || len(legacyFlagAck.body) < 10 || binary.LittleEndian.Uint32(legacyFlagAck.body[len(legacyFlagAck.body)-4:]) != 0 {
		t.Fatalf("unexpected legacy movement-flag ack: %#v", legacyFlagAck)
	}
	controlledLegacyGUID := uint64(0xf130000001000043)
	legacyControl := appendTestLegacyPackedGUID(nil, controlledLegacyGUID)
	legacyControl = append(legacyControl, 1)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGControlUpdate, Body: legacyControl}
	controlUpdate, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	activeMover, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	controlledModernGUID := modernworld.ModernGUIDForLegacy(controlledLegacyGUID, 571)
	if controlUpdate.Opcode != modernworld.SMSGControlUpdate || controlUpdate.Body[len(controlUpdate.Body)-1] != 0x80 || activeMover.Opcode != modernworld.SMSGMoveSetActiveMover {
		t.Fatalf("unexpected control/active-mover packets: control=%#v active=%#v", controlUpdate, activeMover)
	}
	// Empty mover notifications must not reach WotLK, including while a unit
	// is controlled. The next legacy write must be the non-empty mover below.
	if err := instanceClient.WritePacket(modernworld.CMSGSetActiveMover, modernworld.EncodeMoveSetActiveMover(modernworld.GUID128{})); err != nil {
		t.Fatal(err)
	}
	if err := instanceClient.WritePacket(modernworld.CMSGSetActiveMover, modernworld.EncodeMoveSetActiveMover(controlledModernGUID)); err != nil {
		t.Fatal(err)
	}
	setActiveMover := legacyConnection.nextWrite(t)
	if setActiveMover.opcode != legacyworld.CMSGSetActiveMover || len(setActiveMover.body) != 8 || binary.LittleEndian.Uint64(setActiveMover.body) != controlledLegacyGUID {
		t.Fatalf("unexpected legacy set-active-mover write: %#v", setActiveMover)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGUpdateObject, Body: testLegacyValues(0xf130000001000043, map[int]uint32{24: 777})}
	unitValues, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if unitValues.Opcode != modernworld.SMSGUpdateObject || testModernValuesChangedMask(t, unitValues.Body) != 0x20 {
		t.Fatalf("unexpected modern Unit Values update: opcode=%d bytes=%d", unitValues.Opcode, len(unitValues.Body))
	}
	testPVEBatchRelay(t, legacyConnection, instanceClient, controlledLegacyGUID)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGUpdateObject, Body: testLegacyPublicCreate(0x44, 4)}
	otherPlayerCreate, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if otherPlayerCreate.Opcode != modernworld.SMSGUpdateObject || testModernCreatedObjectType(t, otherPlayerCreate.Body) != 6 {
		t.Fatalf("unexpected modern Player create: opcode=%d bytes=%d", otherPlayerCreate.Opcode, len(otherPlayerCreate.Body))
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGUpdateObject, Body: testLegacyStationaryCreate(
		0x4000000000000045, 1, map[int]uint32{3: 6948, 4: math.Float32bits(1), 6: 0x42, 14: 1},
	)}
	itemCreate, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if itemCreate.Opcode != modernworld.SMSGUpdateObject || testModernCreatedObjectType(t, itemCreate.Body) != 1 {
		t.Fatalf("unexpected modern Item create: opcode=%d bytes=%d", itemCreate.Opcode, len(itemCreate.Body))
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGUpdateObject, Body: testLegacyValues(0x4000000000000045, map[int]uint32{60: 25})}
	itemValues, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if itemValues.Opcode != modernworld.SMSGUpdateObject || testModernValuesChangedMask(t, itemValues.Body) != 0x02 {
		t.Fatalf("unexpected modern Item Values update: opcode=%d bytes=%d", itemValues.Opcode, len(itemValues.Body))
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGUpdateObject, Body: testLegacyStationaryCreate(
		0xf110000001000046, 5, map[int]uint32{3: 191747, 4: math.Float32bits(1), 8: 123, 17: 5 | 3<<8},
	)}
	gameObjectCreate, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if gameObjectCreate.Opcode != modernworld.SMSGUpdateObject || testModernCreatedObjectType(t, gameObjectCreate.Body) != 8 {
		t.Fatalf("unexpected modern GameObject create: opcode=%d bytes=%d", gameObjectCreate.Opcode, len(gameObjectCreate.Body))
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGUpdateObject, Body: testLegacyValues(0x42, map[int]uint32{1170: 123456})}
	activeValues, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if activeValues.Opcode != modernworld.SMSGUpdateObject || testModernValuesChangedMask(t, activeValues.Body) != 0x80 {
		t.Fatalf("unexpected modern ActivePlayer Values update: opcode=%d bytes=%d", activeValues.Opcode, len(activeValues.Body))
	}
	standalonePower := appendTestLegacyPackedGUID(nil, 0x42)
	standalonePower = append(standalonePower, 6)
	standalonePower = binary.LittleEndian.AppendUint32(standalonePower, 900)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGPowerUpdate, Body: standalonePower}
	powerUpdate, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if powerUpdate.Opcode != modernworld.SMSGPowerUpdate || binary.LittleEndian.Uint32(powerUpdate.Body[testModernPackedGUIDSize(t, powerUpdate.Body):]) != 1 {
		t.Fatalf("unexpected standalone modern power update: %#v", powerUpdate)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGUpdateObject, Body: testLegacyValues(0x42, map[int]uint32{25: 777})}
	valuesPower, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if valuesPower.Opcode != modernworld.SMSGPowerUpdate || binary.LittleEndian.Uint32(valuesPower.Body[testModernPackedGUIDSize(t, valuesPower.Body)+4:]) != 777 {
		t.Fatalf("unexpected Values power update: %#v", valuesPower)
	}
	standaloneHealth := appendTestLegacyPackedGUID(nil, 0xf130000001000043)
	standaloneHealth = binary.LittleEndian.AppendUint32(standaloneHealth, 555)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGHealthUpdate, Body: standaloneHealth}
	healthUpdate, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	healthGUIDSize := testModernPackedGUIDSize(t, healthUpdate.Body)
	if healthUpdate.Opcode != modernworld.SMSGHealthUpdate || len(healthUpdate.Body) != healthGUIDSize+8 || binary.LittleEndian.Uint64(healthUpdate.Body[healthGUIDSize:]) != 555 {
		t.Fatalf("unexpected standalone modern health update: %#v", healthUpdate)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGUpdateObject, Body: testLegacyFarObjects(0xf110000001000046)}
	outOfRange, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if outOfRange.Opcode != modernworld.SMSGUpdateObject || len(outOfRange.Body) < 17 || outOfRange.Body[6] != 0x80 || binary.LittleEndian.Uint16(outOfRange.Body[7:]) != 0 || binary.LittleEndian.Uint32(outOfRange.Body[9:]) != 1 {
		t.Fatalf("unexpected modern out-of-range update: opcode=%d body=%x", outOfRange.Opcode, outOfRange.Body)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGDestroyObject, Body: binary.LittleEndian.AppendUint64(nil, 0x4000000000000045)}
	destroyObject, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if destroyObject.Opcode != modernworld.SMSGUpdateObject || len(destroyObject.Body) < 17 || destroyObject.Body[6] != 0x80 || binary.LittleEndian.Uint16(destroyObject.Body[7:]) != 1 || binary.LittleEndian.Uint32(destroyObject.Body[9:]) != 1 {
		t.Fatalf("unexpected modern destroy-object update: opcode=%d body=%x", destroyObject.Opcode, destroyObject.Body)
	}
	if err := instanceClient.WritePacket(modernworld.CMSGRequestPlayedTime, []byte{0x80}); err != nil {
		t.Fatal(err)
	}
	playedRequestWrite := legacyConnection.nextWrite(t)
	if playedRequestWrite.opcode != legacyworld.CMSGRequestPlayedTime || !bytes.Equal(playedRequestWrite.body, []byte{1}) {
		t.Fatalf("unexpected legacy played-time request: %#v", playedRequestWrite)
	}
	legacyPlayedTime := binary.LittleEndian.AppendUint32(nil, 123)
	legacyPlayedTime = binary.LittleEndian.AppendUint32(legacyPlayedTime, 45)
	legacyPlayedTime = append(legacyPlayedTime, 1)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGPlayedTime, Body: legacyPlayedTime}
	playedTime, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if playedTime.Opcode != modernworld.SMSGPlayedTime || len(playedTime.Body) != 9 || playedTime.Body[8] != 0x80 {
		t.Fatalf("unexpected modern played-time response: %#v", playedTime)
	}
	if err := instanceClient.WritePacket(modernworld.CMSGMoveInitActiveComplete, []byte{0x78, 0x56, 0x34, 0x12}); err != nil {
		t.Fatal(err)
	}
	activeMoverWrite := legacyConnection.nextWrite(t)
	if activeMoverWrite.opcode != legacyworld.CMSGSetActiveMover || len(activeMoverWrite.body) != 8 || binary.LittleEndian.Uint64(activeMoverWrite.body) != 0x42 {
		t.Fatalf("unexpected legacy active-mover write: %#v", activeMoverWrite)
	}
	playerMovement := modernworld.LegacyMovement{
		MoveFlags: 1, MoveTime: 777, X: 10, Y: 20, Z: 30, Orientation: 1.25, TransportSeat: -1,
	}
	modernMover := modernworld.ModernGUIDForLegacy(0x42, 571)
	if err := instanceClient.WritePacket(modernworld.CMSGMoveHeartbeat, modernworld.EncodeMoveUpdate(playerMovement, modernMover, modernworld.GUID128{})); err != nil {
		t.Fatal(err)
	}
	legacyMovement := legacyConnection.nextWrite(t)
	if legacyMovement.opcode != 0x00ee {
		t.Fatalf("unexpected legacy movement opcode/body: %#v", legacyMovement)
	}
	legacyMover, decodedMovement, err := modernworld.ParseLegacyPlayerMovement(legacyMovement.body)
	if err != nil {
		t.Fatal(err)
	}
	if legacyMover != 0x42 || decodedMovement.MoveTime != 777 || decodedMovement.X != 10 {
		t.Fatalf("unexpected decoded legacy movement: mover=%x move=%#v", legacyMover, decodedMovement)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: 0x00ee, Body: legacyMovement.body}
	moveUpdate, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	decodedModernMovement, err := modernworld.ParseModernPlayerMovement(moveUpdate.Body)
	if err != nil {
		t.Fatal(err)
	}
	if moveUpdate.Opcode != modernworld.SMSGMoveUpdate || decodedModernMovement.Mover != modernMover || decodedModernMovement.Move.MoveTime != 777 {
		t.Fatalf("unexpected modern movement echo: opcode=%d move=%#v", moveUpdate.Opcode, decodedModernMovement)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGTransferPending, Body: binary.LittleEndian.AppendUint32(nil, 33)}
	transferPending, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if transferPending.Opcode != modernworld.SMSGTransferPending || binary.LittleEndian.Uint32(transferPending.Body[:4]) != 33 || len(transferPending.Body) != 17 {
		t.Fatalf("unexpected modern transfer-pending: %#v", transferPending)
	}
	suspendToken, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if suspendToken.Opcode != modernworld.SMSGSuspendToken || !bytes.Equal(suspendToken.Body, modernworld.EncodeSuspendToken(modernworld.DefaultSuspendToken())) {
		t.Fatalf("unexpected suspend token: %#v", suspendToken)
	}
	legacyNewWorld := binary.LittleEndian.AppendUint32(nil, 33)
	for _, value := range []float32{100, 200, 50, 1.5} {
		legacyNewWorld = binary.LittleEndian.AppendUint32(legacyNewWorld, math.Float32bits(value))
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGNewWorld, Body: legacyNewWorld}
	newWorld, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if newWorld.Opcode != modernworld.SMSGNewWorld || binary.LittleEndian.Uint32(newWorld.Body[:4]) != 33 || binary.LittleEndian.Uint32(newWorld.Body[20:24]) != 4 {
		t.Fatalf("unexpected modern new-world: %#v", newWorld)
	}
	lastInstance, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if lastInstance.Opcode != modernworld.SMSGUpdateLastInstance || binary.LittleEndian.Uint32(lastInstance.Body) != 33 {
		t.Fatalf("unexpected last-instance: %#v", lastInstance)
	}
	resumeToken, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if resumeToken.Opcode != modernworld.SMSGResumeToken || !bytes.Equal(resumeToken.Body, suspendToken.Body) {
		t.Fatalf("unexpected resume token: %#v", resumeToken)
	}
	worldServerInfo, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if worldServerInfo.Opcode != modernworld.SMSGWorldServerInfo || binary.LittleEndian.Uint32(worldServerInfo.Body[:4]) != 0 {
		t.Fatalf("unexpected world-server-info: %#v", worldServerInfo)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGTransferPending, Body: binary.LittleEndian.AppendUint32(nil, 0)}
	if err := worldClient.WritePacket(modernworld.CMSGWorldPortResponse, nil); err != nil {
		t.Fatal(err)
	}
	worldPortWrite := legacyConnection.nextWrite(t)
	if worldPortWrite.opcode != legacyworld.MSGMoveWorldportAck || len(worldPortWrite.body) != 0 {
		t.Fatalf("unexpected world-port ack: %#v", worldPortWrite)
	}
	teleportMove := modernworld.LegacyMovement{MoveFlags: 1, MoveTime: 44, X: 8, Y: 9, Z: 10, Orientation: 0.5, TransportSeat: -1}
	teleportMovement, err := modernworld.EncodeLegacyPlayerMovement(modernworld.PlayerMovement{Move: teleportMove}, 0x42, 0)
	if err != nil {
		t.Fatal(err)
	}
	guidPrefix := appendTestLegacyPackedGUID(nil, 0x42)
	legacyTeleport := append([]byte(nil), guidPrefix...)
	legacyTeleport = binary.LittleEndian.AppendUint32(legacyTeleport, 11)
	legacyTeleport = append(legacyTeleport, teleportMovement[len(guidPrefix):]...)
	legacyConnection.reads <- legacyworld.Packet{Opcode: uint16(legacyworld.MSGMoveTeleportAck), Body: legacyTeleport}
	moveTeleport, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if moveTeleport.Opcode != modernworld.SMSGMoveTeleport {
		t.Fatalf("unexpected move teleport opcode %d", moveTeleport.Opcode)
	}
	modernAck := append([]byte(nil), testModernPlayerGUID(0x42)...)
	modernAck = binary.LittleEndian.AppendUint32(modernAck, 11)
	modernAck = binary.LittleEndian.AppendUint32(modernAck, 222)
	if err := instanceClient.WritePacket(modernworld.CMSGMoveTeleportAck, modernAck); err != nil {
		t.Fatal(err)
	}
	teleportAckWrite := legacyConnection.nextWrite(t)
	if teleportAckWrite.opcode != legacyworld.MSGMoveTeleportAck {
		t.Fatalf("unexpected legacy teleport-ack write: %#v", teleportAckWrite)
	}
	// A malformed denial must not stop the relay; subsequent entrance reasons
	// must reach the instance connection with the original UTF-8 text intact.
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGAreaTriggerMessage, Body: []byte{1}}
	for _, message := range []string{"你必须在一个团队中才能进入这个副本。", "你的等级必须达到|CFF9900CC70|R才能进入。"} {
		denial := binary.LittleEndian.AppendUint32(nil, uint32(len(message)+1))
		denial = append(denial, []byte(message)...)
		denial = append(denial, 0)
		legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGAreaTriggerMessage, Body: denial}
		notification, err := instanceClient.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		want := append([]byte{byte(len(message) >> 4), byte(len(message) << 4)}, []byte(message)...)
		if notification.Opcode != modernworld.SMSGPrintNotification || !bytes.Equal(notification.Body, want) {
			t.Fatalf("unexpected entrance notification: %#v", notification)
		}
	}
	legacyAbort := binary.LittleEndian.AppendUint32(nil, 33)
	legacyAbort = append(legacyAbort, 2, 0)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGTransferAborted, Body: legacyAbort}
	aborted, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if aborted.Opcode != modernworld.SMSGTransferAborted || binary.LittleEndian.Uint32(aborted.Body[:4]) != 33 {
		t.Fatalf("unexpected transfer-aborted: %#v", aborted)
	}
	if err := worldClient.WritePacket(modernworld.CMSGLoadingScreenNotify, modernworld.EncodeLoadingScreenNotify(modernworld.LoadingScreenNotify{MapID: 33, Showing: true})); err != nil {
		t.Fatal(err)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGAccountDataTimes, Body: make([]byte, 37)}
	accountTimes, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if accountTimes.Opcode != modernworld.SMSGAccountDataTimes || len(accountTimes.Body) != len(testModernPlayerGUID(0x42))+8+modernworld.AccountDataCount*8 {
		t.Fatalf("unexpected account-data-times packet: opcode=%d bytes=%d", accountTimes.Opcode, len(accountTimes.Body))
	}
	accountUpdate := modernworld.AccountData{
		PlayerGUID:       0x42,
		Time:             1_700_000_001,
		UncompressedSize: 4,
		DataType:         3,
		CompressedData:   []byte{0x78, 0x9c, 1, 2},
	}
	accountUpdateBody, err := modernworld.EncodeAccountDataUpdate(accountUpdate)
	if err != nil {
		t.Fatal(err)
	}
	if err := worldClient.WritePacket(modernworld.CMSGUpdateAccountData, accountUpdateBody); err != nil {
		t.Fatal(err)
	}
	if err := worldClient.WritePacket(modernworld.CMSGRequestAccountData, modernworld.EncodeAccountDataRequest(modernworld.AccountDataRequest{PlayerGUID: 0x42, DataType: 3})); err != nil {
		t.Fatal(err)
	}
	accountReply, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	decodedAccountReply, err := modernworld.ParseAccountDataUpdate(accountReply.Body)
	if err != nil {
		t.Fatal(err)
	}
	if accountReply.Opcode != modernworld.SMSGUpdateAccountData || decodedAccountReply.PlayerGUID != 0x42 || decodedAccountReply.DataType != 3 || decodedAccountReply.Time != accountUpdate.Time || !bytes.Equal(decodedAccountReply.CompressedData, accountUpdate.CompressedData) {
		t.Fatalf("unexpected account-data reply: opcode=%d data=%#v", accountReply.Opcode, decodedAccountReply)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGFeatureSystemStatus, Body: []byte{2, 0}}
	legacyMOTD := binary.LittleEndian.AppendUint32(nil, 1)
	legacyMOTD = append(legacyMOTD, "Welcome to AzerothCore"...)
	legacyMOTD = append(legacyMOTD, 0)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGMOTD, Body: legacyMOTD}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGTutorialFlags, Body: make([]byte, 32)}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGTimeSyncRequest, Body: []byte{7, 0, 0, 0}}
	for _, expected := range []uint16{modernworld.SMSGFeatureSystemStatus, modernworld.SMSGMOTD, modernworld.SMSGTutorialFlags} {
		translated, err := worldClient.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if translated.Opcode != expected || len(translated.Body) == 0 {
			t.Fatalf("unexpected translated realm system packet: opcode=%d bytes=%d, want %d", translated.Opcode, len(translated.Body), expected)
		}
	}
	timeSync, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if timeSync.Opcode != modernworld.SMSGTimeSyncRequest || !bytes.Equal(timeSync.Body, []byte{7, 0, 0, 0}) {
		t.Fatalf("unexpected time-sync request: %#v", timeSync)
	}
	if err := instanceClient.WritePacket(modernworld.CMSGTimeSyncResponse, []byte{7, 0, 0, 0, 0x44, 0x33, 0x22, 0x11}); err != nil {
		t.Fatal(err)
	}
	timeSyncWrite := legacyConnection.nextWrite(t)
	if timeSyncWrite.opcode != legacyworld.CMSGTimeSyncResponse || !bytes.Equal(timeSyncWrite.body, []byte{7, 0, 0, 0, 0x44, 0x33, 0x22, 0x11}) {
		t.Fatalf("unexpected legacy time-sync response: %#v", timeSyncWrite)
	}
	if err := instanceClient.WritePacket(modernworld.CMSGTutorialFlag, modernworld.EncodeTutorialAction(modernworld.TutorialAction{Action: modernworld.TutorialUpdate, Bit: 99})); err != nil {
		t.Fatal(err)
	}
	tutorialWrite := legacyConnection.nextWrite(t)
	if tutorialWrite.opcode != legacyworld.CMSGTutorialFlag || len(tutorialWrite.body) != 4 || binary.LittleEndian.Uint32(tutorialWrite.body) != 99 {
		t.Fatalf("unexpected legacy tutorial update: %#v", tutorialWrite)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGBindPointUpdate, Body: make([]byte, 20)}
	loginTime := binary.LittleEndian.AppendUint32(nil, 0x12345678)
	loginTime = binary.LittleEndian.AppendUint32(loginTime, math.Float32bits(0.01666667))
	loginTime = binary.LittleEndian.AppendUint32(loginTime, 0)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGLoginSetTimeSpeed, Body: loginTime}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGInitializeFactions, Body: make([]byte, 4)}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGSetForcedReactions, Body: make([]byte, 4)}
	for _, expected := range []struct {
		opcode uint16
		bytes  int
	}{
		{modernworld.SMSGBindPointUpdate, 20},
		{modernworld.SMSGLoginSetTimeSpeed, 20},
		{modernworld.SMSGInitializeFactions, 6125},
		{modernworld.SMSGSetForcedReactions, 4},
	} {
		translated, err := instanceClient.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if translated.Opcode != expected.opcode || len(translated.Body) != expected.bytes {
			t.Fatalf("unexpected translated instance init packet: opcode=%d bytes=%d, want opcode=%d bytes=%d", translated.Opcode, len(translated.Body), expected.opcode, expected.bytes)
		}
	}
	// PVE replies must traverse the real legacy reader and encrypted instance
	// socket, with exact wire layouts (not just call the encoder directly).
	for _, tc := range []struct {
		legacy, modern uint16
		in, want       []byte
	}{
		{0x329, 0x26a4, []byte{1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0}, []byte{2, 0, 0, 0}},
		{0x4eb, 0x27ad, []byte{3, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0}, []byte{6, 0, 0, 0, 0}},
		{0x2cb, 0x2780, []byte{1, 0, 0, 0}, []byte{0x80}},
		{0x147, 0x26f8, []byte{0x60, 0xea, 0, 0, 7, 0, 0, 0, 0}, []byte{0x60, 0xea, 0, 0, 7, 0, 0, 0, 0}},
		{0x2fa, 0x2bb4, []byte{4, 0, 0, 0, 0x77, 2, 0, 0, 2, 0, 0, 0, 0x10, 0x0e, 0, 0, 1, 0}, []byte{4, 0x77, 2, 0, 0, 5, 0, 0, 0, 0x80}},
	} {
		legacyConnection.reads <- legacyworld.Packet{Opcode: tc.legacy, Body: tc.in}
		got, err := instanceClient.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if got.Opcode != tc.modern || !bytes.Equal(got.Body, tc.want) {
			t.Fatalf("PVE relay %x: got %#v want %x %x", tc.legacy, got, tc.modern, tc.want)
		}
	}
	// Map difficulty must not generate an extra dungeon-selection reply.
	legacyConnection.reads <- legacyworld.Packet{Opcode: 0x33b, Body: make([]byte, 8)}
	actualDifficulty, err := instanceClient.ReadPacket()
	if err != nil || actualDifficulty.Opcode != modernworld.SMSGWorldServerInfo || binary.LittleEndian.Uint32(actualDifficulty.Body) != 1 {
		t.Fatalf("actual difficulty: %#v %v", actualDifficulty, err)
	}
	if err := instanceClient.WritePacket(modernworld.CMsgPing, []byte{0xef, 0xbe, 0xad, 0xde, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	instancePong, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if instancePong.Opcode != modernworld.SMsgPong || !bytes.Equal(instancePong.Body, []byte{0xef, 0xbe, 0xad, 0xde}) {
		t.Fatalf("unexpected instance pong: %#v", instancePong)
	}
	if err := worldClient.WritePacket(modernworld.CMSGGenerateRandomCharacterName, []byte{1, 0}); err != nil {
		t.Fatal(err)
	}
	randomName, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if randomName.Opcode != modernworld.SMSGGenerateRandomCharacterName || string(randomName.Body) != string([]byte{0, 0}) {
		t.Fatalf("unexpected random-name response: %#v", randomName)
	}
	if err := worldClient.WritePacket(modernworld.CMSGCreateCharacter, testModernCreateCharacter("Newbie")); err != nil {
		t.Fatal(err)
	}
	createWrite := legacyConnection.nextWrite(t)
	if createWrite.opcode != legacyworld.CMSGCreateCharacter || !strings.HasPrefix(string(createWrite.body), "Newbie\x00") {
		t.Fatalf("unexpected legacy create-character request: %#v", createWrite)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGCreateCharacter, Body: []byte{47}}
	if write := legacyConnection.nextWrite(t); write.opcode != legacyworld.CMSGEnumCharacters {
		t.Fatalf("expected internal character enum after create, got %#v", write)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGEnumCharactersResult, Body: testLegacyCharacterEnum("Newbie", 0x43, true)}
	createResult, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	createdGUID, guidErr := modernworld.ParseCharacterGUID(createResult.Body[1:])
	if createResult.Opcode != modernworld.SMSGCreateCharacter || createResult.Body[0] != 24 || guidErr != nil || createdGUID != 0x43 {
		t.Fatalf("unexpected modern create result: opcode=%d body=%x guidErr=%v", createResult.Opcode, createResult.Body, guidErr)
	}
	if err := worldClient.WritePacket(modernworld.CMSGCharDelete, testModernPlayerGUID(0x43)); err != nil {
		t.Fatal(err)
	}
	deleteWrite := legacyConnection.nextWrite(t)
	if deleteWrite.opcode != legacyworld.CMSGCharDelete || len(deleteWrite.body) != 8 || binary.LittleEndian.Uint64(deleteWrite.body) != 0x43 {
		t.Fatalf("unexpected legacy delete-character request: %#v", deleteWrite)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGDeleteCharacter, Body: []byte{71}}
	deleteResult, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if deleteResult.Opcode != modernworld.SMSGDeleteCharacter || string(deleteResult.Body) != string([]byte{63}) {
		t.Fatalf("unexpected modern delete result: %#v", deleteResult)
	}
	if err := worldClient.WritePacket(modernworld.CMsgPing, []byte{0x78, 0x56, 0x34, 0x12, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	pong, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if pong.Opcode != modernworld.SMsgPong || len(pong.Body) != 4 || string(pong.Body) != string([]byte{0x78, 0x56, 0x34, 0x12}) {
		t.Fatalf("unexpected encrypted pong: %#v", pong)
	}
	if err := instanceClient.WritePacket(modernworld.CMSGLogoutRequest, []byte{0}); err != nil {
		t.Fatal(err)
	}
	logoutWrite := legacyConnection.nextWrite(t)
	if logoutWrite.opcode != legacyworld.CMSGLogoutRequest || len(logoutWrite.body) != 0 {
		t.Fatalf("unexpected legacy logout request: %#v", logoutWrite)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGLogoutResponse, Body: []byte{0, 0, 0, 0, 1}}
	logoutResponse, err := instanceClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if logoutResponse.Opcode != modernworld.SMSGLogoutResponse || !bytes.Equal(logoutResponse.Body, []byte{0, 0, 0, 0, 1}) {
		t.Fatalf("unexpected modern logout response: %#v", logoutResponse)
	}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGLogoutComplete}
	logoutComplete, err := worldClient.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if logoutComplete.Opcode != modernworld.SMSGLogoutComplete || len(logoutComplete.Body) != 0 {
		t.Fatalf("unexpected modern logout complete: %#v", logoutComplete)
	}
	_ = instanceRaw.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := instanceClient.ReadPacket(); err == nil {
		t.Fatal("instance connection remained open after logout complete")
	}
	worldSession.worldMu.Lock()
	currentCharacter := worldSession.currentCharacter
	instanceWorld := worldSession.instanceWorld
	worldSession.worldMu.Unlock()
	if currentCharacter != 0 || instanceWorld != nil {
		t.Fatalf("logout state was not reset: character=%x instance=%p", currentCharacter, instanceWorld)
	}
	select {
	case call := <-legacyWorldCalls:
		if call.realmID != 1 || call.account != "TESTER" {
			t.Fatalf("unexpected legacy world call: %#v", call)
		}
	case <-time.After(time.Second):
		t.Fatal("legacy world authentication was not called")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

func assertRPCResponse(t *testing.T, frame bnet.Frame, serviceHash, methodID, token, status uint32) {
	t.Helper()
	if frame.Header.ServiceID != 0xfe || frame.Header.ServiceHash != serviceHash || frame.Header.MethodID != methodID || frame.Header.Token != token || frame.Header.Status != status {
		t.Fatalf("unexpected RPC response header: %#v", frame.Header)
	}
}

func waitForAddresses(t *testing.T, server *Server) map[string]string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		addresses := server.Addresses()
		if len(addresses) == 3 {
			return addresses
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("listeners did not start")
	return nil
}

type fakeLegacyWrite struct {
	opcode uint32
	body   []byte
}

type fakeLegacyWorldConnection struct {
	reads     chan legacyworld.Packet
	writes    chan fakeLegacyWrite
	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeLegacyWorldConnection() *fakeLegacyWorldConnection {
	return &fakeLegacyWorldConnection{
		reads:  make(chan legacyworld.Packet, 16),
		writes: make(chan fakeLegacyWrite, 16),
		closed: make(chan struct{}),
	}
}

func (f *fakeLegacyWorldConnection) ReadPacket() (legacyworld.Packet, error) {
	select {
	case packet := <-f.reads:
		return packet, nil
	case <-f.closed:
		return legacyworld.Packet{}, io.EOF
	}
}

func (f *fakeLegacyWorldConnection) WritePacket(opcode uint32, body []byte) error {
	copyBody := append([]byte(nil), body...)
	select {
	case f.writes <- fakeLegacyWrite{opcode: opcode, body: copyBody}:
		return nil
	case <-f.closed:
		return net.ErrClosed
	}
}

func (f *fakeLegacyWorldConnection) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

func (f *fakeLegacyWorldConnection) nextWrite(t *testing.T) fakeLegacyWrite {
	t.Helper()
	select {
	case write := <-f.writes:
		return write
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for legacy world write")
		return fakeLegacyWrite{}
	}
}

func testModernPlayerGUID(counter byte) []byte {
	// Player GUID: low counter plus HighGuidType703.Player and realm ID 1.
	return []byte{0x01, 0xa0, counter, 0x04, 0x08}
}

func testLegacyActivePlayerCreate(guid uint64) []byte {
	return testLegacyLivingCreate(guid, 4, true)
}

func testLegacyPublicCreate(guid uint64, objectType byte) []byte {
	return testLegacyLivingCreate(guid, objectType, false)
}

func testLegacyMirrorImageCreate(guid uint64) []byte {
	// Field 60 is UNIT_FIELD_FLAGS_2; 0x10 is UNIT_FLAG2_MIRROR_IMAGE.
	return testLegacyLivingCreateFields(guid, 3, false, map[int]uint32{60: 0x810})
}

func testLegacyLivingCreate(guid uint64, objectType byte, self bool) []byte {
	return testLegacyLivingCreateFields(guid, objectType, self, nil)
}

func testLegacyLivingCreateFields(guid uint64, objectType byte, self bool, extra map[int]uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = append(body, 3) // legacy CreateObject2
	body = appendTestLegacyPackedGUID(body, guid)
	body = append(body, objectType)
	updateFlags := uint16(0x20) // living
	if self {
		updateFlags |= 0x01
	}
	body = binary.LittleEndian.AppendUint16(body, updateFlags)
	body = binary.LittleEndian.AppendUint32(body, 0) // movement flags
	body = binary.LittleEndian.AppendUint16(body, 0) // movement extra flags
	body = binary.LittleEndian.AppendUint32(body, 1234)
	for _, value := range []float32{1.25, 2.5, 3.75, 4.5} {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(value))
	}
	body = binary.LittleEndian.AppendUint32(body, 0) // fall time
	for _, value := range []float32{2.5, 7, 4.5, 4.722222, 2.5, 7, 4.5, 3.141593, 3.141593} {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(value))
	}

	fields := map[int]uint32{
		4:    math.Float32bits(1),
		23:   0x010001,
		24:   900,
		32:   1000,
		54:   80,
		55:   1,
		65:   math.Float32bits(0.389),
		66:   math.Float32bits(1.5),
		67:   49,
		68:   49,
		145:  math.Float32bits(1),
		146:  math.Float32bits(1),
		153:  0x02030100,
		154:  0x02010004,
		1279: 80,
	}
	for field, value := range extra {
		fields[field] = value
	}
	mask := make([]uint32, 42)
	for field := range fields {
		mask[field/32] |= uint32(1) << (field % 32)
	}
	body = append(body, byte(len(mask)))
	for _, word := range mask {
		body = binary.LittleEndian.AppendUint32(body, word)
	}
	for field := 0; field < len(mask)*32; field++ {
		if value, ok := fields[field]; ok {
			body = binary.LittleEndian.AppendUint32(body, value)
		}
	}
	return body
}

func testLegacyStationaryCreate(guid uint64, objectType byte, fields map[int]uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = append(body, 3) // legacy CreateObject2
	body = appendTestLegacyPackedGUID(body, guid)
	body = append(body, objectType)
	body = binary.LittleEndian.AppendUint16(body, 0x40) // stationary
	for _, value := range []float32{1.25, 2.5, 3.75, 4.5} {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(value))
	}
	maxField := 0
	for field := range fields {
		if field > maxField {
			maxField = field
		}
	}
	mask := make([]uint32, maxField/32+1)
	for field := range fields {
		mask[field/32] |= uint32(1) << (field % 32)
	}
	body = append(body, byte(len(mask)))
	for _, word := range mask {
		body = binary.LittleEndian.AppendUint32(body, word)
	}
	for field := 0; field < len(mask)*32; field++ {
		if value, ok := fields[field]; ok {
			body = binary.LittleEndian.AppendUint32(body, value)
		}
	}
	return body
}

func testLegacyFarObjects(guids ...uint64) []byte {
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = append(body, 4) // legacy FarObjects
	body = binary.LittleEndian.AppendUint32(body, uint32(len(guids)))
	for _, guid := range guids {
		body = appendTestLegacyPackedGUID(body, guid)
	}
	return body
}

func testLegacyMonsterMove(guid uint64) []byte {
	body := appendTestLegacyPackedGUID(nil, guid)
	body = append(body, 0) // Toggle AnimTierInTrans
	for _, value := range []float32{1.25, 2.5, 3.75} {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(value))
	}
	body = binary.LittleEndian.AppendUint32(body, 7)
	body = append(body, 4) // FacingAngle
	body = binary.LittleEndian.AppendUint32(body, math.Float32bits(1.5))
	body = binary.LittleEndian.AppendUint32(body, 0)    // spline flags
	body = binary.LittleEndian.AppendUint32(body, 1000) // duration
	body = binary.LittleEndian.AppendUint32(body, 1)    // point count
	for _, value := range []float32{4, 5, 6} {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(value))
	}
	return body
}

func testLegacyValues(guid uint64, fields map[int]uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = append(body, 0) // legacy Values
	body = appendTestLegacyPackedGUID(body, guid)
	maxField := 0
	for field := range fields {
		if field > maxField {
			maxField = field
		}
	}
	mask := make([]uint32, maxField/32+1)
	for field := range fields {
		mask[field/32] |= uint32(1) << (field % 32)
	}
	body = append(body, byte(len(mask)))
	for _, word := range mask {
		body = binary.LittleEndian.AppendUint32(body, word)
	}
	for field := 0; field < len(mask)*32; field++ {
		if value, ok := fields[field]; ok {
			body = binary.LittleEndian.AppendUint32(body, value)
		}
	}
	return body
}

func testModernCreatedObjectType(t *testing.T, body []byte) byte {
	t.Helper()
	if len(body) < 15 || binary.LittleEndian.Uint32(body) != 1 {
		t.Fatalf("invalid modern create body: %x", body[:min(len(body), 16)])
	}
	object := body[11:]
	position := 3 // update type plus the two packed-GUID masks
	for _, mask := range object[1:3] {
		for mask != 0 {
			position++
			mask &= mask - 1
		}
	}
	if position >= len(object) {
		t.Fatalf("truncated modern create body: %x", body[:min(len(body), 32)])
	}
	return object[position]
}

func testModernPackedGUIDSize(t *testing.T, body []byte) int {
	t.Helper()
	if len(body) < 2 {
		t.Fatalf("truncated packed GUID128: %x", body)
	}
	size := 2
	for _, mask := range body[:2] {
		for mask != 0 {
			size++
			mask &= mask - 1
		}
	}
	if size > len(body) {
		t.Fatalf("truncated packed GUID128 bytes: %x", body)
	}
	return size
}

func testModernValuesChangedMask(t *testing.T, body []byte) uint32 {
	t.Helper()
	if len(body) < 20 || binary.LittleEndian.Uint32(body) != 1 {
		t.Fatalf("invalid modern Values body: %x", body[:min(len(body), 24)])
	}
	object := body[11:]
	if object[0] != 0 {
		t.Fatalf("modern update type = %d, want Values", object[0])
	}
	position := 3
	for _, mask := range object[1:3] {
		for mask != 0 {
			position++
			mask &= mask - 1
		}
	}
	if position+8 > len(object) {
		t.Fatalf("truncated modern Values object: %x", object)
	}
	return binary.LittleEndian.Uint32(object[position+4:])
}

func appendTestLegacyPackedGUID(dst []byte, guid uint64) []byte {
	var mask byte
	for index := 0; index < 8; index++ {
		if byte(guid>>(8*index)) != 0 {
			mask |= 1 << index
		}
	}
	dst = append(dst, mask)
	for index := 0; index < 8; index++ {
		value := byte(guid >> (8 * index))
		if value != 0 {
			dst = append(dst, value)
		}
	}
	return dst
}

func appendTestModernPackedGUID(dst []byte, guid modernworld.GUID128) []byte {
	lowMask, lowBytes := testPackedUint64(guid.Low)
	highMask, highBytes := testPackedUint64(guid.High)
	dst = append(dst, lowMask, highMask)
	dst = append(dst, lowBytes...)
	return append(dst, highBytes...)
}

func testPackedUint64(value uint64) (byte, []byte) {
	var mask byte
	packed := make([]byte, 0, 8)
	for index := 0; index < 8; index++ {
		part := byte(value >> (8 * index))
		if part != 0 {
			mask |= 1 << index
			packed = append(packed, part)
		}
	}
	return mask, packed
}

func testModernCreateCharacter(name string) []byte {
	body := []byte{byte(len(name) << 2), 0}
	body = append(body, 1, 8, 0) // human, mage, male
	body = binary.LittleEndian.AppendUint32(body, 5)
	body = append(body, name...)
	for _, pair := range [][2]uint32{{9, 17160}, {10, 17172}, {11, 17184}, {12, 17196}, {13, 17206}} {
		body = binary.LittleEndian.AppendUint32(body, pair[0])
		body = binary.LittleEndian.AppendUint32(body, pair[1])
	}
	return body
}

func testLegacyCharacterEnum(name string, guid uint64, firstLogin bool) []byte {
	body := []byte{1}
	body = binary.LittleEndian.AppendUint64(body, guid)
	body = append(body, name...)
	body = append(body, 0)
	body = append(body, 1, 8, 0)       // race, class, sex
	body = append(body, 0, 0, 0, 0, 0) // legacy customizations
	body = append(body, 80)            // level
	body = binary.LittleEndian.AppendUint32(body, 12)
	body = binary.LittleEndian.AppendUint32(body, 571)
	for _, coordinate := range []float32{1, 2, 3} {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(coordinate))
	}
	body = binary.LittleEndian.AppendUint32(body, 0) // guild
	body = binary.LittleEndian.AppendUint32(body, 0) // flags
	body = binary.LittleEndian.AppendUint32(body, 0) // customization flags
	if firstLogin {
		body = append(body, 1)
	} else {
		body = append(body, 0)
	}
	for range 3 {
		body = binary.LittleEndian.AppendUint32(body, 0) // pet fields
	}
	for range 23 {
		body = binary.LittleEndian.AppendUint32(body, 0)
		body = append(body, 0)
		body = binary.LittleEndian.AppendUint32(body, 0)
	}
	return body
}

func TestMailboxCommandsForwardToLegacy(t *testing.T) {
	legacyMailbox := uint64(0xf110000001000042)
	modernMailbox := modernworld.GUID128{Low: 0x42, High: 1}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyMailbox: modernMailbox},
	}

	// CMSG_MAIL_GET_LIST carries the mailbox GUID and must forward the raw
	// 3.3.5a mailbox GUID64.
	body := packTestGUID128(modernMailbox.Low, modernMailbox.High)
	t.Logf("mail get list body=%x", body)
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGMailGetList, Body: body})
	if err != nil || !handled {
		t.Fatalf("mail get list handled=%v err=%v body=%x", handled, err, body)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGMailGetList || binary.LittleEndian.Uint64(write.body) != legacyMailbox {
		t.Fatalf("mail get list write opcode=%#x body=%x", write.opcode, write.body)
	}

	// CMSG_MAIL_DELETE carries no mailbox GUID; the proxy substitutes the
	// tracked current-interacted GameObject.
	session.currentInteractedGO = legacyMailbox
	deleteBody := binary.LittleEndian.AppendUint64(nil, 7) // mail id
	deleteBody = binary.LittleEndian.AppendUint32(deleteBody, 1)
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGMailDelete, Body: deleteBody})
	if err != nil || !handled {
		t.Fatalf("mail delete handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGMailDelete {
		t.Fatalf("mail delete opcode=%#x", write.opcode)
	}
	if len(write.body) != 16 || binary.LittleEndian.Uint64(write.body) != legacyMailbox ||
		binary.LittleEndian.Uint32(write.body[8:12]) != 7 {
		t.Fatalf("mail delete body=%x", write.body)
	}

	// CMSG_MAIL_TAKE_MONEY carries mailbox + mail id + the 3.4.3 i64 gold
	// amount. The amount is dropped; AzerothCore looks the letter up by id.
	takeMoney := packTestGUID128(modernMailbox.Low, modernMailbox.High)
	takeMoney = binary.LittleEndian.AppendUint64(takeMoney, 9)
	takeMoney = binary.LittleEndian.AppendUint64(takeMoney, 5000)
	handled, err = server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGMailTakeMoney, Body: takeMoney})
	if err != nil || !handled {
		t.Fatalf("mail take money handled=%v err=%v", handled, err)
	}
	write = legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGMailTakeMoney {
		t.Fatalf("mail take money opcode=%#x", write.opcode)
	}
	if len(write.body) != 12 || binary.LittleEndian.Uint64(write.body) != legacyMailbox ||
		binary.LittleEndian.Uint32(write.body[8:12]) != 9 {
		t.Fatalf("mail take money body=%x", write.body)
	}
}

func TestMailListResultRelaysToClient(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	legacy := binary.LittleEndian.AppendUint32(nil, 0) // zero records
	legacy = append(legacy, 0)                         // zero mails
	legacyConnection.reads <- legacyworld.Packet{
		Opcode: legacyworld.SMSGMailListResult,
		Body:   legacy,
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("mail list queued %d packets before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}
	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
	session.worldMu.Lock()
	packet := session.pendingInstance[0]
	session.worldMu.Unlock()
	if packet.Opcode != modernworld.SMSGMailListResult || len(packet.Body) != 8 {
		t.Fatalf("mail list packet opcode=%#x body=%x", packet.Opcode, packet.Body)
	}
}

// packTestGUID128 encodes the modern packed-GUID128 wire form (mask bytes then
// the non-zero low/high bytes LSB-first) for the mailbox fixtures above.
func packTestGUID128(low, high uint64) []byte {
	maskByte := func(value uint64) byte {
		var mask byte
		for index := uint(0); index < 8; index++ {
			if byte(value>>(8*index)) != 0 {
				mask |= 1 << index
			}
		}
		return mask
	}
	body := []byte{maskByte(low), maskByte(high)}
	for index := uint(0); index < 8; index++ {
		if value := byte(low >> (8 * index)); value != 0 {
			body = append(body, value)
		}
	}
	for index := uint(0); index < 8; index++ {
		if value := byte(high >> (8 * index)); value != 0 {
			body = append(body, value)
		}
	}
	return body
}

func TestReadItemChainForwardsAndRelays(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
	}

	// CMSG_READ_ITEM is two bag-coordinate bytes and must reach 3.3.5a.
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGReadItem, Body: []byte{0xff, 4}})
	if err != nil || !handled {
		t.Fatalf("read item handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != legacyworld.CMSGReadItem || !bytes.Equal(write.body, []byte{0xff, 4}) {
		t.Fatalf("read item write opcode=%#x body=%x", write.opcode, write.body)
	}

	// Legacy page text response relays as the modern response.
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()
	legacy := binary.LittleEndian.AppendUint32(nil, 0x555)
	legacy = append(legacy, []byte("a page of text\x00")...)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x666)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGQueryPageTextResponse, Body: legacy}

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("page text queued %d packets before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}
	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
	session.worldMu.Lock()
	packet := session.pendingInstance[0]
	session.worldMu.Unlock()
	if packet.Opcode != modernworld.SMSGQueryPageTextResponse {
		t.Fatalf("page text relay opcode=%#x", packet.Opcode)
	}
	if len(packet.Body) < 25 {
		t.Fatalf("page text relay body too short=%x", packet.Body)
	}
}

func TestScatteredWorldPacketsRelay(t *testing.T) {
	const legacyGUID = uint64(0x43)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:             &legacyauth.Session{Username: "TEST"},
		legacyWorld:        legacyConnection,
		currentMapID:       0,
		playerNames:        map[uint64]string{legacyGUID: "Wanderer"},
		playerIdentities:   map[uint64]modernworld.LegacyNameIdentity{legacyGUID: {GUID: legacyGUID, Name: "Wanderer"}},
		knownCharacterInfo: map[uint64]modernworld.LegacyCharacter{legacyGUID: {GUID: legacyGUID, Name: "Keeper"}},
		visibleObjectGUIDs: map[uint64]struct{}{legacyGUID: {}},
		objectGUIDs:        map[uint64]modernworld.GUID128{legacyGUID: {Low: 0x43, High: 9}},
	}
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, legacyConnection)
		close(done)
	}()

	packed := []byte{0x01, 0x43}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGPauseMirrorTimer, Body: []byte{1, 0, 0, 0, 2}}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGPauseMirrorTimer, Body: []byte{1, 0, 0, 0, 1}}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGPlayMusic, Body: []byte{0x34, 0x12, 0, 0}}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGZoneUnderAttack, Body: []byte{0xbe, 0x0d, 0, 0}}
	defense := binary.LittleEndian.AppendUint32(nil, 1519)
	defense = binary.LittleEndian.AppendUint32(defense, uint32(len("Hold")+1))
	defense = append(defense, "Hold\x00"...)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGDefenseMessage, Body: defense}
	serverMessage := binary.LittleEndian.AppendUint32(nil, 3)
	serverMessage = append(serverMessage, "soon\x00"...)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGChatServerMessage, Body: serverMessage}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGQueryItemTextResponse, Body: []byte{1}}
	itemText := append([]byte{0}, packed...)
	itemText = append(itemText, "letter\x00"...)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGQueryItemTextResponse, Body: itemText}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGInvalidatePlayer, Body: packed}
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGSpecialMountAnim, Body: packed}

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == 8 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scattered packets queued %d before timeout", queued)
		}
		time.Sleep(time.Millisecond)
	}
	if err := legacyConnection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}

	session.worldMu.Lock()
	defer session.worldMu.Unlock()
	want := []uint16{
		modernworld.SMSGPauseMirrorTimer,
		modernworld.SMSGPlayMusic,
		modernworld.SMSGZoneUnderAttack,
		modernworld.SMSGDefenseMessage,
		modernworld.SMSGChatServerMessage,
		modernworld.SMSGQueryItemTextResponse,
		modernworld.SMSGInvalidatePlayer,
		modernworld.SMSGSpecialMountAnim,
	}
	if len(session.pendingInstance) != len(want) {
		t.Fatalf("queued %d packets", len(session.pendingInstance))
	}
	for index, opcode := range want {
		if session.pendingInstance[index].Opcode != opcode {
			t.Fatalf("packet %d opcode=%#x want %#x", index, session.pendingInstance[index].Opcode, opcode)
		}
	}
	pause := session.pendingInstance[0].Body
	if len(pause) != 5 || pause[4] != 0x80 {
		t.Fatalf("pause body=%x", pause)
	}
	if !bytes.Equal(session.pendingInstance[1].Body, []byte{0x34, 0x12, 0, 0}) {
		t.Fatalf("music body=%x", session.pendingInstance[1].Body)
	}
	mount := session.pendingInstance[7].Body
	if len(mount) < 8 || binary.LittleEndian.Uint32(mount[len(mount)-8:]) != 0 || binary.LittleEndian.Uint32(mount[len(mount)-4:]) != 0 {
		t.Fatalf("mount body=%x", mount)
	}
	if _, ok := session.playerNames[legacyGUID]; ok {
		t.Fatal("invalidated player name was kept")
	}
	if _, ok := session.playerIdentities[legacyGUID]; ok {
		t.Fatal("invalidated player identity was kept")
	}
	if session.knownCharacterInfo[legacyGUID].Name != "Keeper" {
		t.Fatalf("known character=%#v", session.knownCharacterInfo[legacyGUID])
	}
	if _, visible := session.visibleObjectGUIDs[legacyGUID]; !visible {
		t.Fatal("invalidate-player cleared object visibility")
	}
}

func TestChangeRealmTicketRespondsOnSameConnection(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	dest := modernworld.NewServerConn(right)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
	}
	body := binary.LittleEndian.AppendUint32(nil, 77)
	secret := bytes.Repeat([]byte{0x5a}, 32)
	body = append(body, secret...)

	readDone := make(chan modernworld.Packet, 1)
	readErr := make(chan error, 1)
	go func() {
		packet, err := modernworld.NewClientConn(left).ReadPacket()
		if err != nil {
			readErr <- err
			return
		}
		readDone <- packet
	}()

	handled, err := server.handleModernPlayPacket(session, dest, modernworld.Packet{Opcode: modernworld.CMSGChangeRealmTicket, Body: body})
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	select {
	case err := <-readErr:
		t.Fatal(err)
	case packet := <-readDone:
		if packet.Opcode != modernworld.SMSGChangeRealmTicketResponse {
			t.Fatalf("response opcode=%#x", packet.Opcode)
		}
		if !bytes.Equal(packet.Body, modernworld.EncodeChangeRealmTicketResponse(77)) {
			t.Fatalf("response=%x", packet.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("change-realm-ticket response was not written")
	}
	session.worldMu.Lock()
	secretMatches := bytes.Equal(session.clientSecret[:], secret) && session.hasClientSecret
	session.worldMu.Unlock()
	if !secretMatches {
		t.Fatal("client secret was not stored")
	}
	if len(legacyConnection.writes) != 0 {
		t.Fatalf("ticket was forwarded to the legacy server: %#v", <-legacyConnection.writes)
	}

	if _, err := server.handleModernPlayPacket(session, dest, modernworld.Packet{Opcode: modernworld.CMSGChangeRealmTicket, Body: body[:8]}); err == nil {
		t.Fatal("short change-realm-ticket was accepted")
	}
}
