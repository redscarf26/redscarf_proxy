package modernworld

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestCancelAutoRepeatRoundTrip(t *testing.T) {
	legacy := appendTestPackedGUID(nil, 0xf130000001000043)
	target, err := ParseLegacyCancelAutoRepeat(legacy)
	if err != nil || target != 0xf130000001000043 {
		t.Fatalf("cancel target=%x err=%v", target, err)
	}
	if _, err := ParseLegacyCancelAutoRepeat(append(legacy, 0)); err == nil {
		t.Fatal("cancel with trailing bytes must error")
	}
	body := EncodeCancelAutoRepeat(GUID128{Low: 0x43, High: 1 << 58})
	r := movementReader{data: body}
	guid, err := r.guid128()
	if err != nil || guid != (GUID128{Low: 0x43, High: 1 << 58}) || r.remaining() != 0 {
		t.Fatalf("modern cancel=%#v err=%v", guid, err)
	}
}

func TestEnvironmentalDamageRoundTrip(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130000001000043)
	legacy = append(legacy, 2) // fall damage
	legacy = binary.LittleEndian.AppendUint32(legacy, 500)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)  // resisted
	legacy = binary.LittleEndian.AppendUint32(legacy, 10) // absorbed
	damage, err := ParseLegacyEnvironmentalDamage(legacy)
	if err != nil || damage.Victim != 0xf130000001000043 || damage.Type != 2 || damage.Amount != 500 || damage.Absorbed != 10 {
		t.Fatalf("environmental=%#v err=%v", damage, err)
	}
	body := EncodeEnvironmentalDamage(damage, GUID128{Low: 0x43, High: 1 << 58})
	r := movementReader{data: body}
	guid, _ := r.guid128()
	if guid != (GUID128{Low: 0x43, High: 1 << 58}) {
		t.Fatalf("victim=%#v", guid)
	}
	if typ, _ := r.u8(); typ != 2 {
		t.Fatalf("type=%d", typ)
	}
	if amount, _ := r.u32(); amount != 500 {
		t.Fatalf("amount=%d", amount)
	}
	if resisted, _ := r.u32(); resisted != 0 {
		t.Fatalf("resisted=%d", resisted)
	}
	if absorbed, _ := r.u32(); absorbed != 10 {
		t.Fatalf("absorbed=%d", absorbed)
	}
	if bit, _ := r.bit(); bit {
		t.Fatal("log-data bit set")
	}
}

func TestPetCastFailedRoundTrip(t *testing.T) {
	// No failure args: cast count 0 + spell + reason.
	legacy := []byte{0}
	legacy = binary.LittleEndian.AppendUint32(legacy, 883)
	legacy = append(legacy, 7) // e.g. SPELL_FAILED_NOT_READY
	failure, err := ParseLegacyPetCastFailed(legacy)
	if err != nil || failure.SpellID != 883 || failure.Reason != 7 || failure.Arg1 != -1 || failure.Arg2 != -1 {
		t.Fatalf("pet-cast-failed=%#v err=%v", failure, err)
	}
	body := EncodePetCastFailed(GUID128{Low: 0xabc, High: 1 << 58}, failure)
	r := movementReader{data: body}
	castID, _ := r.guid128()
	if castID != (GUID128{Low: 0xabc, High: 1 << 58}) {
		t.Fatalf("cast id=%#v", castID)
	}
	if spell, _ := r.u32(); spell != 883 {
		t.Fatalf("spell=%d", spell)
	}
	if reason, _ := r.u32(); reason != uint32(ConvertSpellCastResult343(7)) {
		t.Fatalf("reason not converted")
	}
	if arg1, _ := r.i32(); arg1 != -1 {
		t.Fatalf("arg1=%d", arg1)
	}
	if arg2, _ := r.i32(); arg2 != -1 {
		t.Fatalf("arg2=%d", arg2)
	}

	// Two trailing args.
	withArgs := []byte{0}
	withArgs = binary.LittleEndian.AppendUint32(withArgs, 883)
	withArgs = append(withArgs, 7)
	withArgs = binary.LittleEndian.AppendUint32(withArgs, 50)
	withArgs = binary.LittleEndian.AppendUint32(withArgs, 2)
	failure, err = ParseLegacyPetCastFailed(withArgs)
	if err != nil || failure.Arg1 != 50 || failure.Arg2 != 2 {
		t.Fatalf("pet-cast-failed args=%#v err=%v", failure, err)
	}
}

func TestPlaySpellVisualRoundTrip(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130000001000043)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x2A1E)
	caster, kitID, err := ParseLegacyPlaySpellVisual(legacy)
	if err != nil || caster != 0xf130000001000043 || kitID != 0x2A1E {
		t.Fatalf("visual caster=%x kit=%x err=%v", caster, kitID, err)
	}
	body := EncodePlaySpellVisualKit(GUID128{Low: 0x43, High: 1 << 58}, kitID)
	r := movementReader{data: body}
	guid, _ := r.guid128()
	if guid != (GUID128{Low: 0x43, High: 1 << 58}) {
		t.Fatalf("caster=%#v", guid)
	}
	if kit, _ := r.u32(); kit != 0x2A1E {
		t.Fatalf("kit=%x", kit)
	}
	if kitType, _ := r.u32(); kitType != 0 {
		t.Fatalf("kit type=%d", kitType)
	}
	if duration, _ := r.u32(); duration != 0 {
		t.Fatalf("duration=%d", duration)
	}
	if bit, _ := r.bit(); bit {
		t.Fatal("mounted bit set")
	}
}

func TestSpellDamageShieldRoundTrip(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130000001000043)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0xf130005100002345)
	legacy = binary.LittleEndian.AppendUint32(legacy, 123)
	legacy = binary.LittleEndian.AppendUint32(legacy, 40)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	shield, err := ParseLegacySpellDamageShield(legacy)
	if err != nil || shield.SpellID != 123 || shield.Damage != 40 || shield.School != 1 {
		t.Fatalf("shield=%#v err=%v", shield, err)
	}
	body := EncodeSpellDamageShield(shield, GUID128{Low: 0x43, High: 1 << 58}, GUID128{Low: 0x2345, High: 3 << 58})
	r := movementReader{data: body}
	victim, _ := r.guid128()
	caster, _ := r.guid128()
	if victim != (GUID128{Low: 0x43, High: 1 << 58}) || caster != (GUID128{Low: 0x2345, High: 3 << 58}) {
		t.Fatalf("victim=%#v caster=%#v", victim, caster)
	}
	if spell, _ := r.u32(); spell != 123 {
		t.Fatalf("spell=%d", spell)
	}
	if dmg, _ := r.i32(); dmg != 40 {
		t.Fatalf("damage=%d", dmg)
	}
	if orig, _ := r.i32(); orig != 40 {
		t.Fatalf("original=%d", orig)
	}
	if overkill, _ := r.u32(); overkill != 0 {
		t.Fatalf("overkill=%d", overkill)
	}
	if school, _ := r.u32(); school != 1 {
		t.Fatalf("school=%d", school)
	}
	if absorbed, _ := r.u32(); absorbed != 0 {
		t.Fatalf("log absorbed=%d", absorbed)
	}
	if bit, _ := r.bit(); bit {
		t.Fatal("log-data bit set")
	}
}

func TestSpellDispellLogRoundTrip(t *testing.T) {
	legacy := appendTestPackedGUID(nil, 0xf130000001000043)
	legacy = appendTestPackedGUID(legacy, 0xf130005100002345)
	legacy = binary.LittleEndian.AppendUint32(legacy, 111)
	legacy = append(legacy, 0) // debug flag
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	for _, entry := range []struct {
		spell   uint32
		harmful uint8
	}{{777, 0}, {888, 1}} {
		legacy = binary.LittleEndian.AppendUint32(legacy, entry.spell)
		legacy = append(legacy, entry.harmful)
	}
	log, err := ParseLegacySpellDispellLog(legacy)
	if err != nil || log.SpellID != 111 || len(log.DispelledBy) != 2 || !log.DispelledBy[1].Harmful {
		t.Fatalf("dispell=%#v err=%v", log, err)
	}
	body := EncodeSpellDispellLog(log, GUID128{Low: 0x43, High: 1 << 58}, GUID128{Low: 0x2345, High: 3 << 58})
	r := movementReader{data: body}
	if steal, _ := r.bit(); steal {
		t.Fatal("is-steal set")
	}
	if isBreak, _ := r.bit(); isBreak {
		t.Fatal("is-break set")
	}
	target, _ := r.guid128()
	caster, _ := r.guid128()
	if target != (GUID128{Low: 0x43, High: 1 << 58}) || caster != (GUID128{Low: 0x2345, High: 3 << 58}) {
		t.Fatalf("target=%#v caster=%#v", target, caster)
	}
	if spell, _ := r.u32(); spell != 111 {
		t.Fatalf("spell=%d", spell)
	}
	if count, _ := r.i32(); count != 2 {
		t.Fatalf("count=%d", count)
	}
	for i := 0; i < 2; i++ {
		if spellID, _ := r.u32(); spellID != uint32(777+i*111) {
			t.Fatalf("entry %d spell=%d", i, spellID)
		}
		harmful, _ := r.bit()
		rolled, _ := r.bit()
		needed, _ := r.bit()
		if harmful != (i == 1) || rolled || needed {
			t.Fatalf("entry %d bits harmful=%v", i, harmful)
		}
	}
}

func TestSpellInstakillOrderSwap(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130005100002345) // caster first
	legacy = binary.LittleEndian.AppendUint64(legacy, 0xf130000001000043)
	legacy = binary.LittleEndian.AppendUint32(legacy, 40496)
	kill, err := ParseLegacySpellInstakill(legacy)
	if err != nil || kill.Caster != 0xf130005100002345 || kill.Target != 0xf130000001000043 {
		t.Fatalf("instakill=%#v err=%v", kill, err)
	}
	body := EncodeSpellInstakill(kill, GUID128{Low: 0x2345, High: 3 << 58}, GUID128{Low: 0x43, High: 1 << 58})
	r := movementReader{data: body}
	first, _ := r.guid128()
	if first != (GUID128{Low: 0x43, High: 1 << 58}) { // modern writes target first
		t.Fatalf("modern first=%#v want target", first)
	}
	second, _ := r.guid128()
	if second != (GUID128{Low: 0x2345, High: 3 << 58}) {
		t.Fatalf("modern second=%#v", second)
	}
	if spell, _ := r.u32(); spell != 40496 {
		t.Fatalf("spell=%d", spell)
	}
}

func TestTotemCreatedRoundTrip(t *testing.T) {
	legacy := []byte{0} // fire totem slot
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x4000000000000042)
	legacy = binary.LittleEndian.AppendUint32(legacy, 45000)
	legacy = binary.LittleEndian.AppendUint32(legacy, 20608)
	totem, err := ParseLegacyTotemCreated(legacy)
	if err != nil || totem.Slot != 0 || totem.Totem != 0x4000000000000042 || totem.Duration != 45000 || totem.SpellID != 20608 {
		t.Fatalf("totem=%#v err=%v", totem, err)
	}
	body := EncodeTotemCreated(totem, GUID128{Low: 0x42, High: 3 << 58})
	r := movementReader{data: body}
	if slot, _ := r.u8(); slot != 0 {
		t.Fatalf("slot=%d", slot)
	}
	guid, _ := r.guid128()
	if guid != (GUID128{Low: 0x42, High: 3 << 58}) {
		t.Fatalf("totem guid=%#v", guid)
	}
	if duration, _ := r.u32(); duration != 45000 {
		t.Fatalf("duration=%d", duration)
	}
	if spell, _ := r.u32(); spell != 20608 {
		t.Fatalf("spell=%d", spell)
	}
	if timeMod, _ := r.f32(); timeMod != 1.0 {
		t.Fatalf("time mod=%v", timeMod)
	}
	if bit, _ := r.bit(); bit {
		t.Fatal("cannot-dismiss bit set")
	}
}

func TestTotemCreatedTimeModBytes(t *testing.T) {
	// Guard the float encoding is actually the 1.0 bits.
	body := EncodeTotemCreated(LegacyTotemCreated{Slot: 0, Totem: 0x4000000000000042, Duration: 100, SpellID: 9}, GUID128{Low: 0x42, High: 3 << 58})
	// {slot}(1)+packed(0x42/3<<58)+duration(4)+spell(4)+float(4)+bitbyte(1)
	if !isUint32At(body, len(body)-5, math.Float32bits(1.0)) {
		t.Fatalf("totem body tail=%x", body)
	}
}
