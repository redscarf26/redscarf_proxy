package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestSpellCombatLogsAndXPGain(t *testing.T) {
	nonMelee := appendLegacyPackedGUID(nil, 0xf130000001000043)
	nonMelee = appendLegacyPackedGUID(nonMelee, 0x42)
	nonMelee = binary.LittleEndian.AppendUint32(nonMelee, 133)
	nonMelee = binary.LittleEndian.AppendUint32(nonMelee, 25)
	nonMelee = binary.LittleEndian.AppendUint32(nonMelee, 0)
	nonMelee = append(nonMelee, 1)
	nonMelee = binary.LittleEndian.AppendUint32(nonMelee, 0)
	nonMelee = binary.LittleEndian.AppendUint32(nonMelee, 0)
	nonMelee = append(nonMelee, 0, 0)
	nonMelee = binary.LittleEndian.AppendUint32(nonMelee, 0)
	nonMelee = binary.LittleEndian.AppendUint32(nonMelee, 0)
	log, err := ParseLegacySpellNonMeleeDamageLog(nonMelee)
	if err != nil || log.SpellID != 133 || log.Damage != 25 {
		t.Fatalf("non-melee %#v err=%v", log, err)
	}
	withTrail := append(append([]byte{}, nonMelee...), 1, 0, 0, 0, 0)
	if _, err := ParseLegacySpellNonMeleeDamageLog(withTrail); err != nil {
		t.Fatalf("non-melee trailing: %v", err)
	}
	encoded := EncodeSpellNonMeleeDamageLog(GUID128{Low: 0x43, High: 1}, GUID128{Low: 0x42, High: 1}, GUID128{}, log)
	if len(encoded) < 20 {
		t.Fatalf("encoded non-melee %d", len(encoded))
	}

	heal := appendLegacyPackedGUID(nil, 0x42)
	heal = appendLegacyPackedGUID(heal, 0x42)
	heal = binary.LittleEndian.AppendUint32(heal, 2061)
	heal = binary.LittleEndian.AppendUint32(heal, 40)
	heal = binary.LittleEndian.AppendUint32(heal, 0)
	heal = binary.LittleEndian.AppendUint32(heal, 0)
	heal = append(heal, 1)
	healLog, err := ParseLegacySpellHealLog(heal)
	if err != nil || !healLog.Crit || healLog.Amount != 40 {
		t.Fatalf("heal %#v err=%v", healLog, err)
	}
	healDebugFalse := append(append([]byte(nil), heal...), 0)
	if parsed, err := ParseLegacySpellHealLog(healDebugFalse); err != nil || parsed.HasCritRoll {
		t.Fatalf("heal with false debug flag %#v err=%v", parsed, err)
	}
	healDebugTrue := append(append([]byte(nil), heal...), 1)
	healDebugTrue = appendFloat32(healDebugTrue, 0.25)
	healDebugTrue = appendFloat32(healDebugTrue, 0.5)
	parsedDebug, err := ParseLegacySpellHealLog(healDebugTrue)
	if err != nil || !parsedDebug.HasCritRoll || parsedDebug.CritRollMade != 0.25 || parsedDebug.CritRollNeeded != 0.5 {
		t.Fatalf("heal debug %#v err=%v", parsedDebug, err)
	}
	if encodedHeal := EncodeSpellHealLog(GUID128{Low: 0x42}, GUID128{Low: 0x42}, parsedDebug); len(encodedHeal) < 8 {
		t.Fatalf("encoded heal debug is too short: %x", encodedHeal)
	}

	energize := appendLegacyPackedGUID(nil, 0x42)
	energize = appendLegacyPackedGUID(energize, 0x42)
	energize = binary.LittleEndian.AppendUint32(energize, 2687)
	energize = binary.LittleEndian.AppendUint32(energize, 0)
	energize = binary.LittleEndian.AppendUint32(energize, 100)
	energyLog, err := ParseLegacySpellEnergizeLog(energize)
	if err != nil || energyLog.Amount != 100 {
		t.Fatalf("energize %#v err=%v", energyLog, err)
	}

	xp := binary.LittleEndian.AppendUint64(nil, 0xf130000001000043)
	xp = binary.LittleEndian.AppendUint32(xp, 50)
	xp = append(xp, 0)
	xp = binary.LittleEndian.AppendUint32(xp, 10)
	xp = appendFloat32(xp, 1)
	xp = append(xp, 0)
	gain, err := ParseLegacyLogXPGain(xp)
	if err != nil || gain.Original != 50 || gain.Amount != 10 {
		t.Fatalf("xp %#v err=%v", gain, err)
	}
	if EncodeLogXPGain(GUID128{Low: 0x43, High: 1}, gain)[len(EncodeLogXPGain(GUID128{Low: 0x43, High: 1}, gain))-1] != 0 {
		t.Fatal("xp RAF should be 0")
	}
	questXP := binary.LittleEndian.AppendUint64(nil, 0)
	questXP = binary.LittleEndian.AppendUint32(questXP, 500)
	questXP = append(questXP, 1, 1) // non-kill reason plus optional RAF byte
	questGain, err := ParseLegacyLogXPGain(questXP)
	if err != nil || questGain.Original != 500 || questGain.Reason != 1 || questGain.RAFBonus != 1 || questGain.GroupBonus != 1 {
		t.Fatalf("quest xp %#v err=%v", questGain, err)
	}
}

func TestPeriodicAuraAndSpellDelayedTranslation(t *testing.T) {
	periodic := appendLegacyPackedGUID(nil, 0xf130000001000043)
	periodic = appendLegacyPackedGUID(periodic, 0x42)
	periodic = binary.LittleEndian.AppendUint32(periodic, 139)
	periodic = binary.LittleEndian.AppendUint32(periodic, 2)
	periodic = binary.LittleEndian.AppendUint32(periodic, 3) // periodic damage
	periodic = binary.LittleEndian.AppendUint32(periodic, 25)
	periodic = binary.LittleEndian.AppendUint32(periodic, 2)
	periodic = binary.LittleEndian.AppendUint32(periodic, 4)
	periodic = binary.LittleEndian.AppendUint32(periodic, 3)
	periodic = binary.LittleEndian.AppendUint32(periodic, 1)
	periodic = append(periodic, 1)
	periodic = binary.LittleEndian.AppendUint32(periodic, 8) // periodic heal
	periodic = binary.LittleEndian.AppendUint32(periodic, 30)
	periodic = binary.LittleEndian.AppendUint32(periodic, 5)
	periodic = binary.LittleEndian.AppendUint32(periodic, 6)
	periodic = append(periodic, 0)
	log, err := ParseLegacySpellPeriodicAuraLog(periodic)
	if err != nil || len(log.Effects) != 2 || log.Effects[0].OriginalDamage != 25 || !log.Effects[0].Crit || log.Effects[1].AbsorbedOrAmplitude != 6 {
		t.Fatalf("periodic=%#v err=%v", log, err)
	}
	encoded := EncodeSpellPeriodicAuraLog(GUID128{Low: 0x43}, GUID128{Low: 0x42}, log)
	if len(encoded) < 70 {
		t.Fatalf("encoded periodic log is too short: %x", encoded)
	}

	delayedBody := appendLegacyPackedGUID(nil, 0x42)
	delayedBody = binary.LittleEndian.AppendUint32(delayedBody, 750)
	delayed, err := ParseLegacySpellDelayed(delayedBody)
	if err != nil || delayed.Caster != 0x42 || delayed.Delay != 750 {
		t.Fatalf("delayed=%#v err=%v", delayed, err)
	}
	modern := EncodeSpellDelayed(GUID128{Low: 0x42, High: 1}, delayed)
	_, _, consumed, err := readPackedGUID128(modern)
	if err != nil || binary.LittleEndian.Uint32(modern[consumed:]) != 750 {
		t.Fatalf("modern delayed=%x err=%v", modern, err)
	}
}

func TestSpellExecuteLogTranslation(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, 3365)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 33) // open lock
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = appendLegacyPackedGUID(legacy, 0xf110000001000043)
	log, err := ParseLegacySpellExecuteLog(legacy)
	if err != nil || log.Caster != 0x42 || log.SpellID != 3365 || len(log.Effects) != 1 || len(log.Effects[0].GenericVictims) != 1 {
		t.Fatalf("log=%#v err=%v", log, err)
	}
	body := EncodeSpellExecuteLog(log, func(guid uint64) GUID128 {
		return GUID128{Low: guid & 0xffffffff, High: 1}
	})
	_, _, consumed, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	position := consumed + 4 + 4
	if binary.LittleEndian.Uint32(body[position:position+4]) != 33 || binary.LittleEndian.Uint32(body[position+4:position+8]) != 0 || binary.LittleEndian.Uint32(body[position+16:position+20]) != 1 || body[len(body)-1] != 0 {
		t.Fatalf("modern spell-execute=%x", body)
	}

	createItem := appendLegacyPackedGUID(nil, 0x42)
	createItem = binary.LittleEndian.AppendUint32(createItem, 2259)
	createItem = binary.LittleEndian.AppendUint32(createItem, 1)
	createItem = binary.LittleEndian.AppendUint32(createItem, 24)
	createItem = binary.LittleEndian.AppendUint32(createItem, 1)
	createItem = binary.LittleEndian.AppendUint32(createItem, 6948)
	created, err := ParseLegacySpellExecuteLog(createItem)
	if err != nil || len(created.Effects[0].TradeSkillItems) != 1 || created.Effects[0].TradeSkillItems[0] != 6948 {
		t.Fatalf("create-item=%#v err=%v", created, err)
	}
}
