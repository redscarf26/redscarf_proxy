package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestModernMountAuraSpellRewritesWotLKSpeedRanks(t *testing.T) {
	if got := ModernMountAuraSpell(54726); got != 54729 {
		t.Fatalf("winged steed 150%% = %d, want 54729", got)
	}
	if got := ModernMountAuraSpell(54727); got != 54729 {
		t.Fatalf("winged steed 280%% = %d, want 54729", got)
	}
	if got := ModernMountAuraSpell(54729); got != 54729 {
		t.Fatalf("parent mount was rewritten to %d", got)
	}
	if got := ModernMountAuraSpell(8326); got != 8326 {
		t.Fatalf("non-mount spell was rewritten to %d", got)
	}
	if KnownSpellVisual(54726) != 0 || KnownSpellVisual(54727) != 0 || KnownSpellVisual(54729) != 345801 {
		t.Fatalf("3.4.3 visuals 54726=%d 54727=%d 54729=%d", KnownSpellVisual(54726), KnownSpellVisual(54727), KnownSpellVisual(54729))
	}
}

func TestRemapMountSpeedAurasRewritesUnitAuraWire(t *testing.T) {
	auras := []AuraInfo{
		{HasData: true, SpellID: 54726, Flags: auraFlagPositive | auraFlagCancelable},
		{HasData: true, SpellID: 48266, Flags: auraFlagPositive | auraFlagCancelable},
		{Slot: 3},
	}
	RemapMountSpeedAuras(auras)
	if auras[0].SpellID != 54729 || auras[0].VisualID != 0 {
		t.Fatalf("mount aura %#v", auras[0])
	}
	if len(auras[0].Points) != 2 || auras[0].Points[0] != 100 || auras[0].Points[1] != 150 {
		t.Fatalf("mount speed points=%v, want [100 150]", auras[0].Points)
	}
	if auras[1].SpellID != 48266 {
		t.Fatalf("presence aura was rewritten: %#v", auras[1])
	}
	if auras[2].HasData || auras[2].SpellID != 0 {
		t.Fatalf("empty slot was rewritten: %#v", auras[2])
	}

	auras[0].VisualID = KnownSpellVisual(auras[0].SpellID)
	low, high := modernPlayerGUID(0x7)
	body := EncodeAuraUpdate(GUID128{Low: low, High: high}, 530, true, auras[:1])
	if !bytes.Contains(body, binary.LittleEndian.AppendUint32(nil, 54729)) {
		t.Fatalf("encoded aura missing parent 54729: %x", body)
	}
	if !bytes.Contains(body, binary.LittleEndian.AppendUint32(nil, 345801)) {
		t.Fatalf("encoded aura missing parent visual 345801: %x", body)
	}
	if bytes.Contains(body, binary.LittleEndian.AppendUint32(nil, 54726)) {
		t.Fatalf("encoded aura leaked WotLK speed rank 54726: %x", body)
	}
	if !bytes.Contains(body, appendFloat32(nil, 100)) || !bytes.Contains(body, appendFloat32(nil, 150)) {
		t.Fatalf("encoded aura missing 100/150 speed points: %x", body)
	}
}

func TestPartyMountFilterDoesNotRemapSpeedRanks(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 54726)
	legacy = append(legacy, legacyAuraPositive|legacyAuraNoCaster|legacyAuraEffect0)
	r := movementReader{data: legacy}
	auras, err := parseLegacyPartyMemberAuras(&r, "party-member")
	if err != nil {
		t.Fatal(err)
	}
	if len(auras) != 1 || auras[0].SpellID != 54726 {
		t.Fatalf("party stats remapped or dropped speed-rank mount: %#v", auras)
	}
}

func TestCancelAuraUsesMountOpcodeForRemappedParents(t *testing.T) {
	if !CancelAuraUsesMountOpcode(54729) || !CancelAuraUsesMountOpcode(54726) {
		t.Fatal("remapped mount cancel should use CMSG_CANCEL_MOUNT_AURA")
	}
	if CancelAuraUsesMountOpcode(8326) || CancelAuraUsesMountOpcode(48266) {
		t.Fatal("ordinary buff cancel was treated as a mount")
	}
}
