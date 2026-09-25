package modernworld

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"testing"
)

func TestParseCastSpellSelfTarget(t *testing.T) {
	body := appendPackedGUID128(nil, 7, 1)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 133)
	body = binary.LittleEndian.AppendUint32(body, 9)
	body = appendFloat32(body, 0)
	body = appendFloat32(body, 0)
	body = appendPackedGUID128(body, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	bits := newBitWriter(body)
	bits.writeBits(0, 5)
	bits.writeBit(false)
	bits.writeBits(0, 2)
	bits.writeBit(false)
	body = bits.flush()
	targetBits := newBitWriter(body)
	targetBits.writeBits(targetFlagUnit, 28)
	targetBits.writeBit(false)
	targetBits.writeBit(false)
	targetBits.writeBit(false)
	targetBits.writeBit(false)
	targetBits.writeBits(0, 7)
	body = targetBits.flush()
	body = appendPackedGUID128(body, 0x43, 1)
	body = appendPackedGUID128(body, 0, 0)
	request, err := ParseCastSpell(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.SpellID != 133 || request.CastID.Low != 7 || request.Target.Flags != targetFlagUnit || request.Target.Unit.Low != 0x43 {
		t.Fatalf("request %#v", request)
	}
	legacy := EncodeLegacyCastSpell(request, 0xf130000001000043, 0, 0, 0)
	if legacy[0] != 0 || binary.LittleEndian.Uint32(legacy[1:5]) != 133 {
		t.Fatalf("legacy header %x", legacy)
	}
}

func makeUseItemTestPacket(misc [2]uint32, targetFlags uint32) []byte {
	castItem := GUID128{Low: 0x51, High: 1}
	body := []byte{0xff, 35}
	body = appendPackedGUID128(body, castItem.Low, castItem.High)
	body = appendPackedGUID128(body, 7, 1)
	body = binary.LittleEndian.AppendUint32(body, misc[0])
	body = binary.LittleEndian.AppendUint32(body, misc[1])
	body = binary.LittleEndian.AppendUint32(body, 439)
	body = binary.LittleEndian.AppendUint32(body, 17)
	body = appendFloat32(body, 0)
	body = appendFloat32(body, 0)
	body = appendPackedGUID128(body, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	bits := newBitWriter(body)
	bits.writeBits(0, 5)
	bits.writeBit(false)
	bits.writeBits(0, 2)
	bits.writeBit(false)
	body = bits.flush()
	targetBits := newBitWriter(body)
	targetBits.writeBits(targetFlags, 28)
	targetBits.writeBit(false)
	targetBits.writeBit(false)
	targetBits.writeBit(false)
	targetBits.writeBit(false)
	targetBits.writeBits(0, 7)
	body = targetBits.flush()
	body = appendPackedGUID128(body, 0, 0)
	body = appendPackedGUID128(body, 0, 0)
	return body
}

func TestParseAndEncodeUseItem(t *testing.T) {
	castItem := GUID128{Low: 0x51, High: 1}
	body := makeUseItemTestPacket([2]uint32{}, 0)
	request, err := ParseUseItem(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.PackSlot != 0xff || request.Slot != 23 || request.CastItem != castItem || request.Cast.SpellID != 439 {
		t.Fatalf("request %#v", request)
	}
	legacy := EncodeLegacyUseItem(request, 0x4000000000000051, 0, 0, 0, 0)
	if len(legacy) != 24 || legacy[0] != 0xff || legacy[1] != 23 || legacy[2] != 0 {
		t.Fatalf("legacy header %x", legacy)
	}
	if got := binary.LittleEndian.Uint32(legacy[3:7]); got != 439 {
		t.Fatalf("legacy spell ID %d", got)
	}
	if got := binary.LittleEndian.Uint64(legacy[7:15]); got != 0x4000000000000051 {
		t.Fatalf("legacy item GUID %x", got)
	}
	if glyph := binary.LittleEndian.Uint32(legacy[15:19]); glyph != 0 || legacy[19] != 0 {
		t.Fatalf("legacy glyph/flags %x", legacy[15:20])
	}
	if targets := binary.LittleEndian.Uint32(legacy[20:24]); targets != 0 {
		t.Fatalf("legacy targets %x", legacy[20:24])
	}
}

func TestParseUseItemKeepsMOTransportDestGUID(t *testing.T) {
	// ICC gunship hulls are HighGuid::Mo_Transport (0x1fc0). Their modern
	// identity lives entirely in High; Low is always 0.
	const ship = uint64(0x1fc0000000000016)
	transport := ModernGUIDForLegacy(ship, 631)
	if transport.Low != 0 || transport.High == 0 {
		t.Fatalf("MO_TRANSPORT modern GUID=%+v, want Low=0 High!=0", transport)
	}

	body := makeUseItemTestPacket([2]uint32{}, targetFlagDestLoc)
	// makeUseItemTestPacket writes hasDst=false. Rebuild the target block with
	// a dest location relative to the enemy ship.
	castItem := GUID128{Low: 0x51, High: 1}
	body = []byte{0xff, 35}
	body = appendPackedGUID128(body, castItem.Low, castItem.High)
	body = appendPackedGUID128(body, 7, 1)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 68645)
	body = binary.LittleEndian.AppendUint32(body, 351461)
	body = appendFloat32(body, 0)
	body = appendFloat32(body, 0)
	body = appendPackedGUID128(body, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	bits := newBitWriter(body)
	bits.writeBits(0, 5)
	bits.writeBit(false)
	bits.writeBits(0, 2)
	bits.writeBit(false)
	body = bits.flush()
	targetBits := newBitWriter(body)
	targetBits.writeBits(targetFlagDestLoc, 28)
	targetBits.writeBit(false)
	targetBits.writeBit(true)
	targetBits.writeBit(false)
	targetBits.writeBit(false)
	targetBits.writeBits(0, 7)
	body = targetBits.flush()
	body = appendPackedGUID128(body, 0, 0)
	body = appendPackedGUID128(body, 0, 0)
	body = appendPackedGUID128(body, transport.Low, transport.High)
	body = appendFloat32(body, 12.5)
	body = appendFloat32(body, -4)
	body = appendFloat32(body, 8)

	request, err := ParseUseItem(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.Cast.Target.Dst == nil {
		t.Fatal("dest location was dropped")
	}
	if request.Cast.Target.Dst.Transport != transport {
		t.Fatalf("dest transport=%+v, want %+v", request.Cast.Target.Dst.Transport, transport)
	}
	if request.Cast.Target.Dst.Location != (Vec3{X: 12.5, Y: -4, Z: 8}) {
		t.Fatalf("dest location=%+v", request.Cast.Target.Dst.Location)
	}

	legacy := EncodeLegacyUseItem(request, 0x4000000000000ea7, 0, 0, 0, ship)
	if got := binary.LittleEndian.Uint32(legacy[20:24]); got != targetFlagDestLoc {
		t.Fatalf("legacy dest flags=%#x", got)
	}
	wantPacked := appendLegacyPackedGUID(nil, ship)
	gotPacked := legacy[24 : 24+len(wantPacked)]
	if !bytes.Equal(gotPacked, wantPacked) {
		t.Fatalf("legacy dest transport packed=%x, want %x", gotPacked, wantPacked)
	}
}

func TestUseItemPreservesGlyphSocket(t *testing.T) {
	// Exercise the actual modern parser and legacy encoder together. Minor
	// sockets are 1, 2, 4; major sockets are 0, 3, 5 in WotLK GlyphSlot.dbc.
	for slot := uint32(0); slot < 6; slot++ {
		const glyphTarget = uint32(0x00020000)
		request, err := ParseUseItem(makeUseItemTestPacket([2]uint32{slot, 99}, glyphTarget))
		if err != nil {
			t.Fatal(err)
		}
		got := EncodeLegacyUseItem(request, 0x4000000000000051, 0, 0, 0, 0)
		// Fixed legacy header, selected socket, cast flags, target mask.
		want := []byte{0xff, 23, 0, 0xb7, 1, 0, 0, 0x51, 0, 0, 0, 0, 0, 0, 0x40,
			byte(slot), 0, 0, 0, 0, 0, 0, 2, 0}
		if !bytes.Equal(got, want) {
			t.Fatalf("socket %d: legacy packet=%x want=%x", slot, got, want)
		}
	}
}

func TestEncodeLegacyUseItemTrajectory(t *testing.T) {
	request := UseItemRequest{
		PackSlot: 0xff,
		Slot:     23,
		Cast: SpellCastRequest{
			SpellID:       8690,
			SendCastFlags: byte(castFlagHasTrajectory),
			MissilePitch:  1.25,
			MissileSpeed:  30,
		},
	}
	legacy := EncodeLegacyUseItem(request, 0x4000000000000051, 0, 0, 0, 0)
	if len(legacy) != 33 || legacy[19]&byte(castFlagHasTrajectory) == 0 {
		t.Fatalf("legacy trajectory packet %x", legacy)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(legacy[24:28])); got != 1.25 {
		t.Fatalf("legacy pitch=%f", got)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(legacy[28:32])); got != 30 {
		t.Fatalf("legacy speed=%f", got)
	}
	if legacy[32] != 0 {
		t.Fatalf("embedded movement marker=%d", legacy[32])
	}
}

func TestInjectGatheringGameObjectTarget(t *testing.T) {
	target := GUID128{Low: 0x42, High: 10 << 58}
	request := SpellCastRequest{SpellID: 2575}
	if !InjectGatheringGameObjectTarget(&request, target) {
		t.Fatal("mining target was not injected")
	}
	if request.Target.Unit != target || request.Target.Flags&targetFlagGameObject == 0 {
		t.Fatalf("target=%+v flags=%x", request.Target.Unit, request.Target.Flags)
	}
	ordinary := SpellCastRequest{SpellID: 133}
	if InjectGatheringGameObjectTarget(&ordinary, target) {
		t.Fatal("ordinary spell unexpectedly received gathering target")
	}
}

func TestParseCastSpellWithEmbeddedMovement(t *testing.T) {
	body := appendPackedGUID128(nil, 7, 1)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 1752)
	body = binary.LittleEndian.AppendUint32(body, 9)
	body = appendFloat32(body, 0)
	body = appendFloat32(body, 0)
	body = appendPackedGUID128(body, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	bits := newBitWriter(body)
	bits.writeBits(0, 5)
	bits.writeBit(true)
	bits.writeBits(0, 2)
	bits.writeBit(false)
	body = bits.flush()
	targetBits := newBitWriter(body)
	targetBits.writeBits(targetFlagUnit, 28)
	targetBits.writeBit(false)
	targetBits.writeBit(false)
	targetBits.writeBit(false)
	targetBits.writeBit(false)
	targetBits.writeBits(0, 7)
	body = targetBits.flush()
	body = appendPackedGUID128(body, 0x43, 1)
	body = appendPackedGUID128(body, 0, 0)
	move := LegacyMovement{MoveTime: 42, X: -8949.95, Y: -132.5, Z: 83.5, Orientation: 1.2, TransportSeat: -1}
	mover := GUID128{Low: 0x42, High: 1}
	body = append(body, EncodeMoveUpdate(move, mover, GUID128{})...)
	request, err := ParseCastSpell(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.SpellID != 1752 || request.Target.Unit.Low != 0x43 || request.Move == nil {
		t.Fatalf("request %#v", request)
	}
	if request.Move.Mover != mover || request.Move.Move.MoveTime != 42 {
		t.Fatalf("embedded move %#v", request.Move)
	}
	legacy := EncodeLegacyCastSpell(request, 0xf130000001000043, 0, 0, 0)
	if binary.LittleEndian.Uint32(legacy[1:5]) != 1752 {
		t.Fatalf("legacy header %x", legacy)
	}
	legacyMove, err := EncodeLegacyPlayerMovement(*request.Move, 0x42, 0)
	if err != nil || len(legacyMove) == 0 {
		t.Fatalf("legacy move %x err=%v", legacyMove, err)
	}
}

func TestSpellStartAndGoTranslation(t *testing.T) {
	start := appendLegacyPackedGUID(nil, 0x42)
	start = appendLegacyPackedGUID(start, 0x42)
	start = append(start, 0)
	start = binary.LittleEndian.AppendUint32(start, 133)
	start = binary.LittleEndian.AppendUint32(start, 0)
	start = binary.LittleEndian.AppendUint32(start, 1500)
	start = binary.LittleEndian.AppendUint32(start, targetFlagUnit)
	start = appendLegacyPackedGUID(start, 0xf130000001000043)
	parsed, err := ParseLegacySpellStartOrGo(start, false)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SpellID != 133 || parsed.CastTime != 1500 || parsed.Target.Unit.Low != 0xf130000001000043 {
		t.Fatalf("start %#v", parsed)
	}
	parsed.Target.Unit = GUID128{Low: 0x43, High: 1}
	encoded := EncodeSpellStart(parsed, GUID128{Low: 0x42, High: 1}, GUID128{Low: 0x42, High: 1}, ModernCastGUID(0, 133, 7), nil, nil)
	if len(encoded) < 40 {
		t.Fatalf("encoded start too small: %d", len(encoded))
	}

	goBody := appendLegacyPackedGUID(nil, 0x42)
	goBody = appendLegacyPackedGUID(goBody, 0x42)
	goBody = append(goBody, 0)
	goBody = binary.LittleEndian.AppendUint32(goBody, 133)
	goBody = binary.LittleEndian.AppendUint32(goBody, 0)
	goBody = binary.LittleEndian.AppendUint32(goBody, 0)
	goBody = append(goBody, 1)
	goBody = binary.LittleEndian.AppendUint64(goBody, 0xf130000001000043)
	goBody = append(goBody, 0)
	goBody = binary.LittleEndian.AppendUint32(goBody, targetFlagUnit)
	goBody = appendLegacyPackedGUID(goBody, 0xf130000001000043)
	parsedGo, err := ParseLegacySpellStartOrGo(goBody, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsedGo.HitTargets) != 1 || parsedGo.HitTargets[0] != 0xf130000001000043 {
		t.Fatalf("go hits %v", parsedGo.HitTargets)
	}
	parsedGo.Target.Unit = GUID128{Low: 0x43, High: 1}
	parsedGo.VisualID = 0x11223344
	parsedGo.CastFlags = castFlagHasTrajectory | 0x40
	hits := []GUID128{{Low: 0x43, High: 1}}
	encodedGo := EncodeSpellGo(parsedGo, GUID128{Low: 0x42, High: 1}, GUID128{Low: 0x42, High: 1}, ModernCastGUID(0, 133, 7), hits, nil)
	goReader := movementReader{data: encodedGo}
	for range 4 {
		if _, err := goReader.guid128(); err != nil {
			t.Fatal(err)
		}
	}
	spellID, _ := goReader.u32()
	visualID, _ := goReader.u32()
	flags, _ := goReader.u32()
	if spellID != 133 || visualID != 0x11223344 || flags != (castFlagHasTrajectory|0x40) {
		t.Fatalf("spell-go header spell=%d visual=%#x flags=%#x", spellID, visualID, flags)
	}
	if encodedGo[len(encodedGo)-1]&0x80 != 0 {
		t.Fatalf("spell-go log-data bit should be clear: %x", encodedGo)
	}
}

func TestModernCastGUIDMatchesNormalSource(t *testing.T) {
	const (
		mapID   = uint16(42)
		spellID = uint32(836)
		counter = uint64(0x123456789abc)
	)
	got := ModernCastGUID(mapID, spellID, counter)
	wantHigh := uint64(modernHighGuidCast)<<58 |
		uint64(1)<<42 |
		uint64(mapID&0x1fff)<<29 |
		(uint64(spellID)&0x7fffff)<<6 |
		uint64(3) // SpellCastSource.Normal in legacy proxy/Hermes.
	if got.High != wantHigh || got.Low != counter&0xffffffffff {
		t.Fatalf("cast GUID = %#v, want low=%#x high=%#x", got, counter&0xffffffffff, wantHigh)
	}
}

func TestSpellChannelTranslation(t *testing.T) {
	legacyStart := appendLegacyPackedGUID(nil, 0x42)
	legacyStart = binary.LittleEndian.AppendUint32(legacyStart, 746)
	legacyStart = binary.LittleEndian.AppendUint32(legacyStart, 8000)
	start, err := ParseLegacySpellChannelStart(legacyStart)
	if err != nil || start.Caster != 0x42 || start.SpellID != 746 || start.Duration != 8000 {
		t.Fatalf("channel start %#v err=%v", start, err)
	}
	modernStart := EncodeSpellChannelStart(GUID128{Low: 0x42, High: 1}, start.SpellID, 236914, start.Duration)
	r := movementReader{data: modernStart}
	caster, err := r.guid128()
	if err != nil || caster.Low != 0x42 || caster.High != 1 {
		t.Fatalf("modern channel caster %#v err=%v", caster, err)
	}
	spellID, _ := r.u32()
	visualID, _ := r.u32()
	duration, _ := r.u32()
	optionalBits, _ := r.u8()
	if spellID != 746 || visualID != 236914 || duration != 8000 || optionalBits != 0 || r.remaining() != 0 {
		t.Fatalf("modern channel start spell=%d visual=%d duration=%d bits=%d trailing=%d", spellID, visualID, duration, optionalBits, r.remaining())
	}

	legacyUpdate := appendLegacyPackedGUID(nil, 0x42)
	legacyUpdate = binary.LittleEndian.AppendUint32(legacyUpdate, 0)
	update, err := ParseLegacySpellChannelUpdate(legacyUpdate)
	if err != nil || update.Caster != 0x42 || update.TimeRemaining != 0 {
		t.Fatalf("channel update %#v err=%v", update, err)
	}
	modernUpdate := EncodeSpellChannelUpdate(GUID128{Low: 0x42, High: 1}, update.TimeRemaining)
	ur := movementReader{data: modernUpdate}
	updateCaster, err := ur.guid128()
	remaining, remainingErr := ur.u32()
	if err != nil || remainingErr != nil || updateCaster.Low != 0x42 || remaining != 0 || ur.remaining() != 0 {
		t.Fatalf("modern channel update caster=%#v remaining=%d err=%v/%v trailing=%d", updateCaster, remaining, err, remainingErr, ur.remaining())
	}
}

func TestSpellModifierTranslation(t *testing.T) {
	legacy := []byte{0, 14}
	legacy = binary.LittleEndian.AppendUint32(legacy, ^uint32(4))
	modifier, err := ParseLegacySpellModifier(legacy)
	if err != nil || modifier.ClassIndex != 0 || modifier.ModIndex != 14 || modifier.Value != -5 {
		t.Fatalf("modifier %#v err=%v", modifier, err)
	}
	modern := EncodeSpellModifier(modifier)
	if len(modern) != 14 || binary.LittleEndian.Uint32(modern[0:4]) != 1 || modern[4] != 14 ||
		binary.LittleEndian.Uint32(modern[5:9]) != 1 || int32(binary.LittleEndian.Uint32(modern[9:13])) != -5 || modern[13] != 0 {
		t.Fatalf("modern modifier %x", modern)
	}
	if _, err := ParseLegacySpellModifier(append(legacy, 0)); err == nil {
		t.Fatal("spell modifier with trailing data was accepted")
	}
}

func TestResurrectRequestResponse(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130000001000009)
	legacy = binary.LittleEndian.AppendUint32(legacy, 5)
	legacy = append(legacy, "Bob\x00"...)
	legacy = append(legacy, 1, 0)
	request, err := ParseLegacyResurrectRequest(legacy)
	if err != nil || request.Caster != 0xf130000001000009 || request.Name != "Bob" || !request.Sickness || request.UseTimer {
		t.Fatalf("request %#v err=%v", request, err)
	}
	body := EncodeResurrectRequest(GUID128{Low: 9, High: 1}, 1, request)
	if len(body) < 10 {
		t.Fatalf("encoded resurrect %x", body)
	}
	guid, response, err := ParseResurrectResponse(append(appendPackedGUID128(nil, 9, 1), 1, 0, 0, 0))
	if err != nil || guid.Low != 9 || response != 1 {
		t.Fatalf("response guid=%#v response=%d err=%v", guid, response, err)
	}
	legacyResp := EncodeLegacyResurrectResponse(0xf130000001000009, true)
	if len(legacyResp) != 9 || legacyResp[8] != 0 {
		t.Fatalf("legacy accept %x", legacyResp)
	}
	if EncodeLegacyResurrectResponse(1, false)[8] != 1 {
		t.Fatal("legacy decline should be 1")
	}
}

func TestCountNonZeroActionButtons(t *testing.T) {
	if got := CountNonZeroActionButtons([]int32{0, 133, 0, 168}); got != 2 {
		t.Fatalf("filled=%d", got)
	}
	buttons := []int32{133, 0x01000001, 168}
	if got := ActionButtonSpellIDs(buttons); len(got) != 2 || got[0] != 133 || got[1] != 168 {
		t.Fatalf("spell ids %v", got)
	}
	if got := MissingKnownSpells([]uint32{133, 168, 6603}, []uint32{133, 1752, 133}); len(got) != 1 || got[0] != 1752 {
		t.Fatalf("missing %v", got)
	}
}

func TestCancelCastAuraAndSpellFailure(t *testing.T) {
	castID := ModernCastGUID(0, 133, 7)
	body := appendPackedGUID128(nil, castID.Low, castID.High)
	body = binary.LittleEndian.AppendUint32(body, 133)
	request, err := ParseCancelCast(body)
	if err != nil || request.SpellID != 133 {
		t.Fatalf("cancel-cast %#v err=%v", request, err)
	}
	legacy := EncodeLegacyCancelCast(133)
	if len(legacy) != 5 || legacy[0] != 0 || binary.LittleEndian.Uint32(legacy[1:]) != 133 {
		t.Fatalf("legacy cancel-cast %x", legacy)
	}
	aura := binary.LittleEndian.AppendUint32(nil, 8326)
	aura = appendPackedGUID128(aura, 1, 1)
	spellID, caster, err := ParseCancelAura(aura)
	if err != nil || spellID != 8326 || caster.Low != 1 {
		t.Fatalf("cancel-aura spell=%d caster=%#v err=%v", spellID, caster, err)
	}
	failLegacy := appendLegacyPackedGUID(nil, 0x42)
	failLegacy = append(failLegacy, 1)
	failLegacy = binary.LittleEndian.AppendUint32(failLegacy, 133)
	failLegacy = append(failLegacy, 4)
	info, err := ParseLegacySpellFailure(failLegacy, false)
	if err != nil || info.SpellID != 133 || info.Reason != 4 {
		t.Fatalf("failure %#v err=%v", info, err)
	}
	failed := EncodeSpellFailure(GUID128{Low: 0x42, High: 1}, castID, info, 0)
	if len(failed) < 10 {
		t.Fatalf("spell-failure %d", len(failed))
	}
	castFailedLegacy := []byte{1, 133, 0, 0, 0, 25}
	castFailed, err := ParseLegacyCastFailed(castFailedLegacy)
	if err != nil || castFailed.SpellID != 133 || castFailed.Reason != 25 {
		t.Fatalf("cast-failed %#v err=%v", castFailed, err)
	}
	if ConvertSpellCastResult343(16) != 19 || ConvertSpellCastResult343(25) != 29 {
		t.Fatalf("spell-cast-result 16=%d 25=%d", ConvertSpellCastResult343(16), ConvertSpellCastResult343(25))
	}
	encodedFail := EncodeCastFailed(GUID128{Low: 1, High: 1}, SpellFailureInfo{SpellID: 133, Reason: 25, Arg1: -1, Arg2: -1}, 0)
	if binary.LittleEndian.Uint32(encodedFail[len(encodedFail)-12:]) != 29 {
		t.Fatalf("encoded cast-failed reason %x", encodedFail)
	}
	failedOther := EncodeSpellFailedOther(GUID128{Low: 0x42, High: 1}, castID, info, 0)
	if len(failedOther) != len(failed)-1 {
		t.Fatalf("spell-failed-other len=%d, spell-failure len=%d (reason should be uint8 vs uint16)", len(failedOther), len(failed))
	}
	if got := failedOther[len(failedOther)-1]; got != byte(ConvertSpellCastResult343(info.Reason)) {
		t.Fatalf("spell-failed-other reason byte=%d want=%d", got, ConvertSpellCastResult343(info.Reason))
	}
}

func TestSpellGoSetsHasTrajectory(t *testing.T) {
	cast := SpellCastData{SpellID: 7268, CastFlags: 0x40, CastTime: 0}
	encoded := EncodeSpellGo(cast, GUID128{Low: 0x42, High: 1}, GUID128{Low: 0x42, High: 1}, ModernCastGUID(0, 7268, 1), nil, nil)
	r := movementReader{data: encoded}
	for range 4 {
		if _, err := r.guid128(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	flags, err := r.u32()
	if err != nil {
		t.Fatal(err)
	}
	if flags&castFlagHasTrajectory == 0 {
		t.Fatalf("spell-go flags=%#x missing HasTrajectory", flags)
	}
}

func readSpellGoFlags(t *testing.T, encoded []byte) uint32 {
	t.Helper()
	r := movementReader{data: encoded}
	for range 4 {
		if _, err := r.guid128(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	flags, err := r.u32()
	if err != nil {
		t.Fatal(err)
	}
	return flags
}

func TestSpellGoDestWithoutTravelDoesNotForceTrajectory(t *testing.T) {
	cast := SpellCastData{
		SpellID: 69832,
		Target: SpellCastTargets{
			Flags: targetFlagDestLoc,
			Dst:   &SpellTargetLocation{Location: Vec3{X: 1, Y: 2, Z: 3}},
		},
	}
	encoded := EncodeSpellGo(cast, GUID128{Low: 0x42, High: 1}, GUID128{Low: 0x42, High: 1}, ModernCastGUID(631, 69832, 1), nil, nil)
	if flags := readSpellGoFlags(t, encoded); flags&castFlagHasTrajectory != 0 {
		t.Fatalf("dest missile with TravelTime 0 still has HasTrajectory flags=%#x", flags)
	}
}

func TestSpellGoDestWithTravelKeepsHasTrajectory(t *testing.T) {
	cast := SpellCastData{
		SpellID:    69832,
		TravelTime: 3600,
		Target: SpellCastTargets{
			Flags: targetFlagDestLoc,
			Dst:   &SpellTargetLocation{Location: Vec3{X: 1, Y: 2, Z: 3}},
		},
	}
	encoded := EncodeSpellGo(cast, GUID128{Low: 0x42, High: 1}, GUID128{Low: 0x42, High: 1}, ModernCastGUID(631, 69832, 1), nil, nil)
	if flags := readSpellGoFlags(t, encoded); flags&castFlagHasTrajectory == 0 {
		t.Fatalf("dest missile with TravelTime %d missing HasTrajectory flags=%#x", cast.TravelTime, flags)
	}
}

func TestNeedsFallingMissileTrajectoryOnlyRotfaceExplosion(t *testing.T) {
	if !NeedsFallingMissileTrajectory(69832) {
		t.Fatal("69832 must keep the falling dest missile")
	}
	if !NeedsFallingMissileTrajectory(69846) {
		t.Fatal("69846 must keep the falling dest missile")
	}
	for _, spellID := range []uint32{69833, 69839, 69845, 70022, 70346, 70341, 70542, 70447} {
		if NeedsFallingMissileTrajectory(spellID) {
			t.Fatalf("spell %d must not get a falling dest missile", spellID)
		}
	}
}

func TestApplyTriggeredMissileTrajectoryFallsOntoDest(t *testing.T) {
	cast := SpellCastData{
		SpellID: 69832,
		Target: SpellCastTargets{
			Flags: targetFlagDestLoc,
			Dst:   &SpellTargetLocation{Location: Vec3{X: 10, Y: 20, Z: 5}},
		},
	}
	ApplyTriggeredMissileTrajectory(&cast, &[3]float32{0, 0, 5}, nil, true)
	if cast.Target.Src == nil || cast.Target.Src.Location != (Vec3{X: 10, Y: 20, Z: 35}) {
		t.Fatalf("src %#v", cast.Target.Src)
	}
	if cast.TravelTime != 3750 {
		t.Fatalf("travel %d want 3750", cast.TravelTime)
	}
	if cast.Target.Flags&targetFlagSourceLoc == 0 {
		t.Fatalf("src flag missing %#x", cast.Target.Flags)
	}
}

func TestApplyTriggeredMissileTrajectoryFliesFromCasterForPlayerTarget(t *testing.T) {
	cast := SpellCastData{
		SpellID: 69832,
		Target: SpellCastTargets{
			Flags: targetFlagDestLoc,
			Dst:   &SpellTargetLocation{Location: Vec3{X: 16, Y: 0, Z: 0}},
		},
	}
	ApplyTriggeredMissileTrajectory(&cast, &[3]float32{0, 0, 0}, nil, false)
	if cast.Target.Src == nil || cast.Target.Src.Location != (Vec3{X: 0, Y: 0, Z: 0}) {
		t.Fatalf("src %#v", cast.Target.Src)
	}
	if cast.TravelTime != 2000 {
		t.Fatalf("travel %d want 2000", cast.TravelTime)
	}
}

func TestApplyTriggeredMissileTrajectoryLeavesExistingTravel(t *testing.T) {
	cast := SpellCastData{
		TravelTime: 900,
		Target: SpellCastTargets{
			Dst: &SpellTargetLocation{Location: Vec3{X: 10, Y: 0, Z: 0}},
		},
	}
	ApplyTriggeredMissileTrajectory(&cast, &[3]float32{0, 0, 0}, nil, true)
	if cast.TravelTime != 900 || cast.Target.Src != nil {
		t.Fatalf("existing trajectory was rewritten: travel=%d src=%#v", cast.TravelTime, cast.Target.Src)
	}
}

func TestEncodeSpellGoWritesDestTransportGUID(t *testing.T) {
	const ship = uint64(0x1fc0000000000017)
	transport := ModernGUIDForLegacy(ship, 631)
	cast := SpellCastData{
		SpellID: 68645,
		Target: SpellCastTargets{
			Flags: targetFlagDestLoc,
			Dst: &SpellTargetLocation{
				Transport: transport,
				Location:  Vec3{X: 6.5, Y: 1, Z: 20.5},
			},
		},
	}
	encoded := EncodeSpellGo(cast, GUID128{Low: 0x12}, GUID128{Low: 0x12}, ModernCastGUID(631, 68645, 1), nil, nil)
	if !bytes.Contains(encoded, appendPackedGUID128(nil, transport.Low, transport.High)) {
		t.Fatalf("spell-go missing dest transport %016x:%016x in %x", transport.High, transport.Low, encoded)
	}
}

func TestSpellGoCarriesRemainingRunes(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0x42)
	legacy = appendLegacyPackedGUID(legacy, 0x42)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 45477)
	legacy = binary.LittleEndian.AppendUint32(legacy, castFlagRuneList)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = append(legacy, 0, 0) // hit and miss counts
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = append(legacy, 0x3f, 0x3e, 0x40)

	cast, err := ParseLegacySpellStartOrGo(legacy, true)
	if err != nil {
		t.Fatal(err)
	}
	if cast.RemainingRunes == nil {
		t.Fatal("legacy rune list was discarded")
	}
	wantRunes := []byte{0x3f, 0x3e, 6, 0, 0, 0, 0x40, 0xff, 0xff, 0xff, 0xff, 0xff}
	if got := EncodeRuneData(*cast.RemainingRunes); !bytes.Equal(got, wantRunes) {
		t.Fatalf("remaining runes = %x, want %x", got, wantRunes)
	}
	wantPower := []SpellPowerData{
		{Cost: 1, Type: powerRuneBlood},
		{Cost: 2, Type: powerRuneFrost},
		{Cost: 2, Type: powerRuneUnholy},
	}
	if !reflect.DeepEqual(cast.RemainingPower, wantPower) {
		t.Fatalf("remaining power = %+v, want %+v", cast.RemainingPower, wantPower)
	}

	encoded := EncodeSpellGo(cast, GUID128{Low: 0x42, High: 1}, GUID128{Low: 0x42, High: 1}, ModernCastGUID(0, cast.SpellID, 1), nil, nil)
	// SpellGo appends one final bitfield byte after SpellCastData. RemainingPower
	// sits immediately before RemainingRunes, matching legacy proxy SpellGo 224343/779.
	wantTail := append([]byte{}, EncodeSpellPowerData(wantPower[0])...)
	wantTail = append(wantTail, EncodeSpellPowerData(wantPower[1])...)
	wantTail = append(wantTail, EncodeSpellPowerData(wantPower[2])...)
	wantTail = append(wantTail, wantRunes...)
	if !bytes.Equal(encoded[len(encoded)-len(wantTail)-1:len(encoded)-1], wantTail) {
		t.Fatalf("spell-go rune tail = %x, want %x in %x", encoded[len(encoded)-len(wantTail)-1:len(encoded)-1], wantTail, encoded)
	}
}

func TestSpellGoRewritesZeroRuneCooldown(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0x42)
	legacy = appendLegacyPackedGUID(legacy, 0x42)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 45477)
	legacy = binary.LittleEndian.AppendUint32(legacy, castFlagRuneList)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = append(legacy, 0, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = append(legacy, 0x3f, 0x2f, 0)

	cast, err := ParseLegacySpellStartOrGo(legacy, true)
	if err != nil {
		t.Fatal(err)
	}
	if cast.RemainingRunes == nil || cast.RemainingRunes.Cooldowns[4] != 1 {
		t.Fatalf("just-spent cooldown = %v, want 1", cast.RemainingRunes)
	}
}

func TestUpdateMissileTrajectoryRoundTrip(t *testing.T) {
	body := appendPackedGUID128(nil, 0x42, 1)
	body = appendPackedGUID128(body, 7, 1)
	body = binary.LittleEndian.AppendUint16(body, 3)
	body = binary.LittleEndian.AppendUint32(body, 7268)
	body = appendFloat32(body, 0.25)
	body = appendFloat32(body, 40)
	body = appendFloat32(body, 1)
	body = appendFloat32(body, 2)
	body = appendFloat32(body, 3)
	body = appendFloat32(body, 4)
	body = appendFloat32(body, 5)
	body = appendFloat32(body, 6)
	parsed, err := ParseUpdateMissileTrajectory(body)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SpellID != 7268 || parsed.Guid.Low != 0x42 || parsed.Speed != 40 || parsed.ImpactPos.Z != 6 {
		t.Fatalf("parsed %#v", parsed)
	}
	legacy := EncodeLegacyUpdateMissileTrajectory(0x42, parsed)
	if len(legacy) != 8+4+4+4+12+12+1 {
		t.Fatalf("legacy missile trajectory len=%d", len(legacy))
	}
	if binary.LittleEndian.Uint64(legacy[:8]) != 0x42 || binary.LittleEndian.Uint32(legacy[8:12]) != 7268 || legacy[len(legacy)-1] != 0 {
		t.Fatalf("legacy missile trajectory %x", legacy)
	}
	if _, err := ParseUpdateMissileTrajectory(body[:10]); err == nil {
		t.Fatal("short missile trajectory was accepted")
	}
}

func TestParseCancelMountAura(t *testing.T) {
	if err := ParseCancelMountAura(nil); err != nil {
		t.Fatal(err)
	}
	if err := ParseCancelMountAura([]byte{0}); err == nil {
		t.Fatal("cancel-mount-aura with a payload was accepted")
	}
}
