package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

func FormatLegacyValuesFields(fields map[int]uint32) string {
	if len(fields) == 0 {
		return ""
	}
	keys := make([]int, 0, len(fields))
	for field := range fields {
		keys = append(keys, field)
	}
	sort.Ints(keys)
	parts := make([]string, 0, len(keys))
	for _, field := range keys {
		parts = append(parts, fmt.Sprintf("%d=0x%x", field, fields[field]))
	}
	return strings.Join(parts, ",")
}

// LegacyUnitTargetValue returns the 64-bit WotLK UNIT_FIELD_TARGET value and
// whether either half of that two-field GUID was present in the supplied map.
func LegacyUnitTargetValue(fields map[int]uint32) (uint64, bool) {
	low, lowOK := fields[legacyUnitTarget]
	high, highOK := fields[legacyUnitTarget+1]
	return uint64(low) | uint64(high)<<32, lowOK || highOK
}

// LegacyChannelObject is UNIT_FIELD_CHANNEL_OBJECT from a cached values map.
func LegacyChannelObject(fields map[int]uint32) uint64 {
	if fields == nil {
		return 0
	}
	return uint64(fields[legacyUnitChannelObject]) | uint64(fields[legacyUnitChannelObject+1])<<32
}

// ApplyLegacyChannelState writes UNIT_CHANNEL_SPELL and CHANNEL_OBJECT into a
// cached values map so later deltas merge against the pose the client holds.
func ApplyLegacyChannelState(fields map[int]uint32, spellID uint32, channelObject uint64) {
	if fields == nil {
		return
	}
	fields[legacyUnitChannelSpell] = spellID
	fields[legacyUnitChannelObject] = uint32(channelObject)
	fields[legacyUnitChannelObject+1] = uint32(channelObject >> 32)
}

// SpellChannelObject prefers a SpellGo hit GUID, then the packed unit target.
func SpellChannelObject(cast SpellCastData) uint64 {
	for _, hit := range cast.HitTargets {
		if hit != 0 {
			return hit
		}
	}
	if cast.Target.Unit.Low != 0 {
		return cast.Target.Unit.Low
	}
	return 0
}

// EncodeUnitChannelValuesUpdate builds a standalone Values blob so MSG_CHANNEL_START
// can hold the 3.4 channeling pose. ChannelData on the unit, not the channel
// packet, is what raises both hands; WotLK often sends the packet first.
func EncodeUnitChannelValuesUpdate(guid GUID128, mapID uint16, objectType uint8, spellID uint32, channelObject uint64) ([]byte, error) {
	if objectType != 3 && objectType != 4 {
		objectType = 3
	}
	fields := map[int]uint32{
		legacyUnitChannelSpell:      spellID,
		legacyUnitChannelObject:     uint32(channelObject),
		legacyUnitChannelObject + 1: uint32(channelObject >> 32),
	}
	body, _, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type:   LegacyUpdateValues,
		Values: LegacyUpdateValuesBlock{Fields: fields},
	}, ValuesUpdateOptions{MapID: mapID, ObjectType: objectType, GUID: guid, Fields: fields})
	return body, err
}

// ValuesUpdateOptions supplies object-cache context which is not carried in a
// legacy Values update itself.
type ValuesUpdateOptions struct {
	MapID      uint16
	ObjectType uint8
	GUID       GUID128
	Active     bool
	Fields     map[int]uint32 // merged create state plus the current delta
	// StateOnlyQuests marks quests whose only wire objective is the synthesized
	// StorageIndex 0 AreaTrigger. translateQuestLogStateFlags adds StateFlags
	// bit 0x100 for them once their slot reaches the COMPLETE state.
	StateOnlyQuests map[uint32]struct{}
}

// EncodeValuesUpdate translates the currently useful subset of WotLK update
// fields into build-54261 changed-mask sections. A nil body means that the
// delta contained no field for which a safe modern mapping is implemented.
// EncodeValuesUpdate reports fields that actually produce a descriptor delta,
// not the size of the legacy input. Audit shares the encoder, so zero suppression
// and object/class-dependent mappings cannot drift from a second field table.
func EncodeValuesUpdate(update LegacyObjectUpdate, options ValuesUpdateOptions) ([]byte, int, error) {
	body, _, err := encodeValuesUpdate(update, options)
	if err != nil || len(body) == 0 {
		return body, 0, err
	}
	if len(update.Values.Fields) == 1 {
		return body, 1, nil
	}
	report := AuditValuesFields(update, options)
	return body, len(report.Emitted), nil
}

type ValuesFieldAudit struct{ Emitted, Deferred []int }

// Deferred includes both unimplemented fields and deliberate compatibility
// suppression. It must not be described as an unsupported-field count.
func AuditValuesFields(update LegacyObjectUpdate, options ValuesUpdateOptions) ValuesFieldAudit {
	var report ValuesFieldAudit
	for field, value := range update.Values.Fields {
		// These fixed ActivePlayer arrays unconditionally emit both halves.
		// Avoid re-encoding the full 128-record SkillInfo mask for each word.
		if options.Active && options.ObjectType == 4 && update.Type == LegacyUpdateValues && options.GUID != (GUID128{}) &&
			(field >= legacyPlayerSkill1 && field < legacyPlayerSkill1+384 || field >= legacyPlayerExplored1 && field < legacyPlayerExplored1+128 || field >= legacyPlayerKnownTitles && field < legacyPlayerKnownTitles+6) {
			report.Emitted = append(report.Emitted, field)
			continue
		}
		one := update
		one.Values = LegacyUpdateValuesBlock{Fields: map[int]uint32{field: value}}
		body, _, err := encodeValuesUpdate(one, options)
		if err == nil && len(body) != 0 {
			report.Emitted = append(report.Emitted, field)
		} else {
			report.Deferred = append(report.Deferred, field)
		}
	}
	sort.Ints(report.Emitted)
	sort.Ints(report.Deferred)
	return report
}

func encodeValuesUpdate(update LegacyObjectUpdate, options ValuesUpdateOptions) ([]byte, int, error) {
	if update.Type != LegacyUpdateValues {
		return nil, 0, fmt.Errorf("update type is %d, want legacy Values", update.Type)
	}
	if options.GUID.Low == 0 && options.GUID.High == 0 {
		return nil, 0, fmt.Errorf("modern object GUID is empty")
	}
	changed, fields := update.Values.Fields, options.Fields
	if stealthProbe && options.Active {
		changed, fields = applyStealthProbe(changed, fields)
	}
	values := legacyActivePlayerValues{fields: fields}
	var changedMask uint32
	sections := make([][]byte, 0, 7)

	if section := encodeObjectValuesDelta(values, changed, options.ObjectType, update.GUID); len(section) != 0 {
		changedMask |= 0x01
		sections = append(sections, section)
	}
	if options.ObjectType == 1 || options.ObjectType == 2 {
		if section := encodeItemValuesDelta(values, changed, options.MapID); len(section) != 0 {
			changedMask |= 0x02
			sections = append(sections, section)
		}
	}
	if options.ObjectType == 2 {
		if section := encodeContainerValuesDelta(values, changed, options.MapID); len(section) != 0 {
			changedMask |= 0x04
			sections = append(sections, section)
		}
	}
	if options.ObjectType == 3 || options.ObjectType == 4 {
		if section := encodeUnitValuesDelta(values, changed, options.ObjectType, options.MapID); len(section) != 0 {
			changedMask |= 0x20
			sections = append(sections, section)
		}
	}
	if options.ObjectType == 4 {
		if section := encodePlayerValuesDelta(values, changed, options.MapID, options.StateOnlyQuests, options.Active); len(section) != 0 {
			changedMask |= 0x40
			sections = append(sections, section)
		}
		if options.Active {
			if section := encodeActivePlayerValuesDelta(values, changed, options.MapID); len(section) != 0 {
				changedMask |= 0x80
				sections = append(sections, section)
			}
		}
	}
	if options.ObjectType == 5 {
		if section := encodeGameObjectValuesDelta(values, changed, options.MapID); len(section) != 0 {
			changedMask |= 0x100
			sections = append(sections, section)
		}
	}
	if options.ObjectType == 6 {
		if section := encodeDynamicObjectValuesDelta(values, changed, options.MapID); len(section) != 0 {
			changedMask |= 0x200
			sections = append(sections, section)
		}
	}
	if changedMask == 0 {
		return nil, 0, nil
	}

	valuesData := binary.LittleEndian.AppendUint32(nil, changedMask)
	for _, section := range sections {
		valuesData = append(valuesData, section...)
	}
	objectData := []byte{0} // UpdateTypeModern.Values
	objectData = appendPackedGUID128(objectData, options.GUID.Low, options.GUID.High)
	objectData = binary.LittleEndian.AppendUint32(objectData, uint32(len(valuesData)))
	objectData = append(objectData, valuesData...)
	return encodeUpdateObjects(options.MapID, objectData), len(changed), nil
}

// stealthProbe is a temporary diagnostic, enabled with REDSCARF_STEALTH_PROBE=1.
// A stealth Values blob that also carried Object/ActivePlayer sections was
// dropped whole (sit and scale never landed). The probe now only flips
// StandState inside the Unit section: if the character sits when stealthing,
// that section is reaching the client.
var stealthProbe = os.Getenv("REDSCARF_STEALTH_PROBE") == "1"

func applyStealthProbe(changed, merged map[int]uint32) (map[int]uint32, map[int]uint32) {
	raw, ok := changed[legacyUnitBytes1]
	if !ok {
		return changed, merged
	}
	stand := uint32(0)
	if raw&(uint32(unitByte1VisCreep)<<16) != 0 {
		stand = 1
	}
	changedCopy, mergedCopy := cloneFields(changed), cloneFields(merged)
	changedCopy[legacyUnitBytes1] = raw
	mergedCopy[legacyUnitBytes1] = merged[legacyUnitBytes1]&^0xff | stand
	return changedCopy, mergedCopy
}

func cloneFields(fields map[int]uint32) map[int]uint32 {
	clone := make(map[int]uint32, len(fields)+2)
	for field, value := range fields {
		clone[field] = value
	}
	return clone
}

func encodeObjectValuesDelta(values legacyActivePlayerValues, changed map[int]uint32, objectType uint8, legacyGUID uint64) []byte {
	var mask uint32
	if _, ok := changed[legacyObjectEntry]; ok {
		mask |= 1 << 1
	}
	var dynamic uint32
	dynamicChanged := false
	switch objectType {
	case 3, 4:
		if _, ok := changed[legacyUnitDynamicFlags]; ok {
			dynamic = modernUnitDynamicFlags(values.field(legacyUnitDynamicFlags))
			// Creature DynamicFlags must carry zero as well: that is how the
			// server clears UNIT_DYNFLAG_LOOTABLE after a corpse is emptied.
			// Player stealth Values always include field 79=0, however, and a
			// Values blob with Object+Unit+ActivePlayer is dropped whole, so
			// retain the zero-value suppression only for player objects.
			dynamicChanged = objectType == 3 || dynamic != 0
		}
	case 5:
		_, dynamicChanged = changed[legacyGameObjectDynamic]
		transport := isLegacyTransportGUID(legacyGUID)
		if transport {
			_, stateChanged := changed[legacyGameObjectBytes1]
			dynamicChanged = dynamicChanged || stateChanged
		}
		if dynamicChanged {
			dynamic = modernGameObjectDynamicFlags(values.field(legacyGameObjectDynamic), transport)
			if transport {
				dynamic = transportStateDynamicFlags(dynamic, values.fields)
			}
		}
	}
	if dynamicChanged {
		mask |= 1 << 2
	}
	if _, ok := changed[legacyObjectScale]; ok {
		mask |= 1 << 3
	}
	if mask == 0 {
		return nil
	}
	mask |= 1
	bits := newBitWriter(nil)
	bits.writeBits(mask, 4)
	dst := bits.flush()
	if mask&(1<<1) != 0 {
		dst = binary.LittleEndian.AppendUint32(dst, values.field(legacyObjectEntry))
	}
	if dynamicChanged {
		dst = binary.LittleEndian.AppendUint32(dst, dynamic)
	}
	if mask&(1<<3) != 0 {
		dst = binary.LittleEndian.AppendUint32(dst, values.field(legacyObjectScale))
	}
	return dst
}

type valueDelta struct {
	bit  int
	data []byte
}

// unitPowerSlot returns the modern UnitData Power[] slot for a legacy power
// type. Player units keep the per-class layout (classPowerSlot); a creature with
// DisplayPower Focus is a hunter pet and stores focus at slot 0 and happiness at
// slot 3, matching legacy proxy's create and feed-tick Values output.
func unitPowerSlot(objectType uint8, values legacyActivePlayerValues, class byte, legacyType int) int {
	if objectType == 3 && byte(values.field(legacyUnitBytes0)>>24) == 2 {
		switch legacyType {
		case 2: // focus
			return 0
		case 4: // happiness
			return 3
		}
		return -1
	}
	return classPowerSlot(class, legacyType)
}

func encodeUnitValuesDelta(values legacyActivePlayerValues, changed map[int]uint32, objectType uint8, mapID uint16) []byte {
	deltas := make([]valueDelta, 0, 32)
	if objectType == 4 {
		if _, changedGuild := changed[legacyPlayerGuildID]; changedGuild {
			deltas = append(deltas, valueDelta{107, guildAppendGUID(nil, GuildGUID(values.field(legacyPlayerGuildID)))})
		}
	}
	add32 := func(bit, field int) {
		deltas = append(deltas, valueDelta{bit, binary.LittleEndian.AppendUint32(nil, values.field(field))})
	}
	addByte := func(bit int, value byte) { deltas = append(deltas, valueDelta{bit, []byte{value}}) }
	if _, ok := changed[legacyUnitHealth]; ok {
		deltas = append(deltas, valueDelta{5, binary.LittleEndian.AppendUint64(nil, uint64(modernUnitHealth(values)))})
	}
	if _, ok := changed[legacyUnitMaxHealth]; ok {
		deltas = append(deltas, valueDelta{6, binary.LittleEndian.AppendUint64(nil, uint64(values.field(legacyUnitMaxHealth)))})
	}
	// WotLK stores the six Unit control relationships as adjacent uint64
	// fields. Build 54261 exposes them as PackedGuid128 descriptors 11..16.
	// Charm is especially important for the hunter taming quests: without it
	// the client never exposes the temporary controlled creature and cannot
	// send CMSG_PET_ABANDON before using the next quest rod.
	for _, mapping := range []struct {
		field int
		bit   int
	}{
		{legacyUnitCharm, 11},
		{legacyUnitSummon, 12},
		{legacyUnitCritter, 13},
		{legacyUnitCharmedBy, 14},
		{legacyUnitSummonedBy, 15},
		{legacyUnitCreatedBy, 16},
	} {
		_, lowChanged := changed[mapping.field]
		_, highChanged := changed[mapping.field+1]
		if lowChanged || highChanged {
			deltas = append(deltas, valueDelta{mapping.bit, values.appendLegacyGUID(nil, mapping.field, mapID)})
		}
	}
	_, targetLowChanged := changed[legacyUnitTarget]
	_, targetHighChanged := changed[legacyUnitTarget+1]
	if targetLowChanged || targetHighChanged {
		// 3.3.5 UNIT_FIELD_TARGET occupies two uint32 fields (18/19). Build
		// 54261 represents it as UnitData.Target, update bit 19, PackedGuid128.
		deltas = append(deltas, valueDelta{19, values.appendLegacyGUID(nil, legacyUnitTarget, mapID)})
	}
	_, channelObjectLowChanged := changed[legacyUnitChannelObject]
	_, channelObjectHighChanged := changed[legacyUnitChannelObject+1]
	channelObjectChanged := channelObjectLowChanged || channelObjectHighChanged
	channelObject := values.legacyGUID(legacyUnitChannelObject)
	if channelObjectChanged {
		// Build 54261 moved UNIT_FIELD_CHANNEL_OBJECT into UnitData's
		// ChannelObjects dynamic field (bit 4). Its size/update mask lives in
		// the changed-mask bit stream; the GUID body is present only for a
		// non-empty collection. Keeping an empty delta is required to clear a
		// previous channel target when the legacy server writes zero.
		var payload []byte
		if channelObject != 0 {
			payload = values.appendLegacyGUID(nil, legacyUnitChannelObject, mapID)
		}
		deltas = append(deltas, valueDelta{4, payload})
	}
	if _, ok := changed[legacyUnitChannelSpell]; ok {
		// UnitData.ChannelData is an inline composite without an inner mask.
		// The modern client uses both values to select and hold the channeling
		// pose; forwarding only MSG_CHANNEL_START leaves item channels idle.
		spellID := values.field(legacyUnitChannelSpell)
		payload := binary.LittleEndian.AppendUint32(nil, spellID)
		payload = binary.LittleEndian.AppendUint32(payload, KnownSpellVisual(spellID))
		deltas = append(deltas, valueDelta{22, payload})
	}
	for field, bit := range map[int]int{
		legacyUnitDisplayID: 7, legacyUnitFaction: 40, legacyUnitRangedAttackTime: 45,
		legacyUnitBaseAttackTime: 172, legacyUnitBaseAttackTime + 1: 173,
		legacyUnitAuraState: 44, legacyUnitBoundingRadius: 46, legacyUnitCombatReach: 47,
		legacyUnitNativeDisplayID: 49, legacyUnitMountDisplayID: 51,
		legacyUnitMinDamage: 52, legacyUnitMinDamage + 1: 53, legacyUnitMinDamage + 2: 54, legacyUnitMinDamage + 3: 55,
		legacyUnitPetNumber: 60, legacyUnitPetNumber + 1: 61, legacyUnitPetNumber + 2: 62, legacyUnitPetNumber + 3: 63,
		legacyUnitModCastSpeed: 65, legacyUnitCreatedBySpell: 71, legacyUnitEmoteState: 72,
		legacyUnitBaseMana: 75, legacyUnitBaseHealth: 76, legacyUnitAttackPower: 81,
		legacyUnitAttackPowerMult: 84, legacyUnitRangedAttackPower: 85, legacyUnitRangedAPMult: 88,
		legacyUnitMinRangedDamage: 91, legacyUnitMinRangedDamage + 1: 92,
		legacyUnitMaxHealthModifier: 93, legacyUnitHoverHeight: 94,
	} {
		if _, ok := changed[field]; ok {
			add32(bit, field)
		}
	}
	if _, ok := changed[legacyUnitLevel]; ok {
		add32(30, legacyUnitLevel)
		add32(31, legacyUnitLevel)
	}
	if _, ok := changed[legacyUnitFlags]; ok {
		deltas = append(deltas, valueDelta{9, binary.LittleEndian.AppendUint32(nil, modernUnitStateAnimID(values.field(legacyUnitFlags)))})
		deltas = append(deltas, valueDelta{41, binary.LittleEndian.AppendUint32(nil, modernUnitFlags(values.field(legacyUnitFlags)))})
	}
	if _, ok := changed[legacyUnitFlags2]; ok {
		deltas = append(deltas, valueDelta{42, binary.LittleEndian.AppendUint32(nil, values.field(legacyUnitFlags2))})
	}
	if _, ok := changed[legacyUnitBytes0]; ok {
		raw := values.field(legacyUnitBytes0)
		impersonating := objectType == 3 && values.field(legacyUnitFlags2)&0x10 != 0
		if impersonating {
			addByte(24, 0)
			addByte(25, 0)
			addByte(27, 0)
		} else {
			addByte(24, byte(raw))
			addByte(25, byte(raw>>8))
			addByte(27, byte(raw>>16))
		}
		addByte(28, byte(raw>>24))
	}
	if _, ok := changed[legacyUnitBytes1]; ok {
		raw := values.field(legacyUnitBytes1)
		// legacy proxy WriteUpdateUnitData WriteByte parent 32: StandState 56,
		// PetTalent 57, VisFlags 58, AnimTier 59. Bits 66-69 are floats.
		stand, pet, vis, anim := modernUnitBytes1(raw)
		for bit, value := range []byte{stand, pet, vis, anim} {
			addByte(56+bit, value)
		}
	}
	if _, ok := changed[legacyUnitBytes2]; ok {
		raw := values.field(legacyUnitBytes2)
		for offset, bit := range []int{77, 78, 79, 80} {
			addByte(bit, byte(raw>>(8*offset)))
		}
	}
	for field, bitsPair := range map[int][2]int{
		legacyUnitAttackPowerMods: {82, 83}, legacyUnitRangedAPMods: {86, 87},
	} {
		if _, ok := changed[field]; ok {
			raw := values.field(field)
			for index, bit := range bitsPair {
				value := int32(int16(raw >> (16 * index)))
				deltas = append(deltas, valueDelta{bit, binary.LittleEndian.AppendUint32(nil, uint32(value))})
			}
		}
	}
	class := byte(values.field(legacyUnitBytes0) >> 8)
	for legacyType := 0; legacyType < 7; legacyType++ {
		slot := unitPowerSlot(objectType, values, class, legacyType)
		if slot < 0 {
			continue
		}
		if _, ok := changed[40+legacyType]; ok {
			add32(117+slot, 40+legacyType)
		}
		if _, ok := changed[47+legacyType]; ok {
			add32(127+slot, 47+legacyType)
		}
		if _, ok := changed[legacyUnitPower1+legacyType]; ok {
			add32(137+slot, legacyUnitPower1+legacyType)
		}
		if _, ok := changed[legacyUnitMaxPower1+legacyType]; ok {
			add32(147+slot, legacyUnitMaxPower1+legacyType)
		}
	}
	// Equipment changes arrive as ordinary VALUES updates. The create path
	// already maps these owner-only fields, but omitting them here leaves the
	// character sheet at its pre-equip values until the player is recreated.
	for index := 0; index < 5; index++ {
		for _, mapping := range []struct {
			field int
			bit   int
		}{
			{legacyUnitStat0 + index, 175 + index},
			{legacyUnitPositiveStat0 + index, 180 + index},
			{legacyUnitNegativeStat0 + index, 185 + index},
		} {
			if _, ok := changed[mapping.field]; ok {
				add32(mapping.bit, mapping.field)
			}
		}
	}
	for index := 0; index < 7; index++ {
		for _, mapping := range []struct {
			field int
			bit   int
		}{
			{legacyUnitResistance0 + index, 191 + index},
			{legacyUnitPositiveResist0 + index, 213 + index},
			{legacyUnitNegativeResist0 + index, 220 + index},
			{legacyUnitPowerCostModifier0 + index, 198 + index},
			{legacyUnitPowerCostMult0 + index, 205 + index},
		} {
			if _, ok := changed[mapping.field]; ok {
				add32(mapping.bit, mapping.field)
			}
		}
	}
	if value, ok := changed[legacyUnitNPCFlags]; ok && (objectType == 3 || value != 0) {
		// Creature NPCFlags must carry zero: escort/follow quest acceptance
		// clears the quest giver's flag this way, which removes the overhead
		// exclamation mark. Active-player updates still suppress their routine
		// zero field so stealth stays on the known-good Unit-only delta path.
		add32(114, legacyUnitNPCFlags)
	}
	for index := 0; index < 3; index++ {
		virtualField := legacyUnitVirtualItem1 + index
		visibleField := legacyPlayerVisibleItem1 + (15+index)*2
		_, virtualChanged := changed[virtualField]
		_, visibleChanged := changed[visibleField]
		if !virtualChanged && !visibleChanged {
			continue
		}
		itemID := values.field(virtualField)
		if itemID == 0 {
			itemID = values.field(visibleField)
		}
		// Hermes VirtualItem::WriteUpdate: 4-bit mask 0x03 (hasAny+ItemID) + Int32.
		bits := newBitWriter(nil)
		bits.writeBits(0x03, 4)
		payload := bits.flush()
		payload = binary.LittleEndian.AppendUint32(payload, itemID)
		deltas = append(deltas, valueDelta{168 + index, payload})
	}
	return encodeBlockDeltas(deltas, 8, 8, false, func(bits *bitWriter, blocks []uint32) {
		if bits == nil {
			setUnitParentBits(blocks, deltas)
			return
		}
		sort.Slice(deltas, func(i, j int) bool { return unitDeltaOrder(deltas[i].bit) < unitDeltaOrder(deltas[j].bit) })
		if channelObjectChanged {
			if channelObject != 0 {
				bits.writeBits(1, 32) // ChannelObjects size
				bits.writeBit(true)   // element 0 changed
			} else {
				bits.writeBits(0, 32) // clear the dynamic field
			}
		}
	})
}

// unitFieldGroups are the UnitData block bits: each
// entry is a parent bit and the last child bit that belongs to it. The client
// only reads a child value when the parent bit is set, so a delta without its
// parent silently shifts every following field.
var unitFieldGroups = [...][2]int{
	{0, 31}, {32, 63}, {64, 95}, {96, 112}, {113, 115}, {116, 166},
	{167, 170}, {171, 173}, {174, 189}, {190, 211}, {212, 228},
}

func unitParentBit(bit int) int {
	for _, group := range unitFieldGroups {
		if bit > group[0] && bit <= group[1] {
			return group[0]
		}
	}
	return -1
}

func setUnitParentBits(blocks []uint32, deltas []valueDelta) {
	for _, delta := range deltas {
		if parent := unitParentBit(delta.bit); parent >= 0 {
			setMaskBit(blocks, parent)
		}
	}
}

func encodePlayerValuesDelta(values legacyActivePlayerValues, changed map[int]uint32, mapID uint16, areaOnlyQuests map[uint32]struct{}, active bool) []byte {
	deltas := make([]valueDelta, 0, 32)
	add32 := func(bit int, value uint32) {
		deltas = append(deltas, valueDelta{bit, binary.LittleEndian.AppendUint32(nil, value)})
	}
	if _, changedGuild := changed[legacyPlayerGuildID]; changedGuild {
		level := uint32(0)
		if values.field(legacyPlayerGuildID) != 0 {
			level = 25
		}
		add32(11, level)
	}
	// PLAYER_FLAGS_GHOST keeps the same 0x10 value in 3.3.5 and 3.4.3.
	// It must be sent on the live-player transition too: the modern client
	// uses this PlayerData flag to enter and leave its ghost world view.
	if _, ok := changed[legacyPlayerFlags]; ok {
		legacy := values.field(legacyPlayerFlags)
		add32(7, modernPlayerFlags(legacy))
		var flagsEx uint32
		if legacy&0x400 != 0 {
			flagsEx |= 0x80
		}
		if legacy&0x800 != 0 {
			flagsEx |= 0x100
		}
		add32(8, flagsEx)
	}
	for field, bit := range map[int]int{
		legacyPlayerGuildRank: 9, legacyPlayerDuelTeam: 19,
		legacyPlayerGuildTimestamp: 20, legacyPlayerChosenTitle: 21,
	} {
		if _, ok := changed[field]; ok {
			add32(bit, values.field(field))
		}
	}
	for slot := 0; slot < 25; slot++ {
		base := legacyPlayerQuestLog1 + slot*5
		changedSlot := false
		for offset := 0; offset < 5; offset++ {
			if _, ok := changed[base+offset]; ok {
				changedSlot = true
			}
		}
		if !changedSlot {
			continue
		}
		payload := appendQuestLogSlot(nil, values.field(base), values.field(base+1), values.field(base+2), values.field(base+3), values.field(base+4), areaOnlyQuests)
		deltas = append(deltas, valueDelta{36 + slot, payload})
	}
	for slot := 0; slot < 19; slot++ {
		base := legacyPlayerVisibleItem1 + slot*2
		_, entryChanged := changed[base]
		_, enchantChanged := changed[base+1]
		if !entryChanged && !enchantChanged {
			continue
		}
		if active && !entryChanged {
			// Temp-enchant (whetstone) VALUES on the owner used to disconnect
			// the 3.4.3 client (error 7). Equip still writes ItemID and must
			// reach PlayerData.VisibleItems so the local model updates.
			continue
		}
		// legacy proxy WriteUpdateVisibleItem: 4-bit HasChangesMask (hasAny/ItemID/
		// Appearance/Visual) + FlushBits + Int32 + UInt16 + UInt16. Hermes
		// 3.4.3 writes the same form with mask 0x0F. CREATE omits the nibble.
		bits := newBitWriter(nil)
		bits.writeBits(0x0f, 4)
		payload := bits.flush()
		payload = binary.LittleEndian.AppendUint32(payload, values.field(base))
		payload = binary.LittleEndian.AppendUint16(payload, 0)
		payload = binary.LittleEndian.AppendUint16(payload, visibleItemEnchantVisual(values.field(base+1)))
		deltas = append(deltas, valueDelta{62 + slot, payload})
	}
	parents := func(blocks []uint32) {
		for _, delta := range deltas {
			switch {
			case delta.bit >= 1 && delta.bit <= 34:
				setMaskBit(blocks, 0)
			case delta.bit >= 36 && delta.bit <= 60:
				setMaskBit(blocks, 35)
			case delta.bit >= 62 && delta.bit <= 80:
				setMaskBit(blocks, 61)
			}
		}
	}
	return encodeBlockDeltas(deltas, 3, 4, false, func(bits *bitWriter, blocks []uint32) {
		parents(blocks)
		if bits != nil {
			bits.writeBit(true)
		}
	})
}

const (
	// ActivePlayerData.AuraVision: whether this player sees through stealth.
	// Field layout puts it at bit 103 (parent 102), between
	// LocalRegenFlags and NumBackpackSlots; bit 114 is PvpRankProgress.
	playerAuraVisionBit       = 103
	playerAuraVisionParentBit = 102
	// ActivePlayerData.OverrideSpellsID: SPELL_AURA_OVERRIDE_SPELLS set id.
	// Same parent 102; CREATE writes it immediately after NumBackpackSlots.
	playerOverrideSpellsBit = 105
)

func encodeActivePlayerValuesDelta(values legacyActivePlayerValues, changed map[int]uint32, mapID uint16) []byte {
	deltas := make([]valueDelta, 0, 32)
	add32 := func(bit, field int) {
		deltas = append(deltas, valueDelta{bit, binary.LittleEndian.AppendUint32(nil, values.field(field))})
	}
	addExpertise := func(bit, field int) {
		data := appendFloat32(nil, float32(int32(values.field(field))))
		deltas = append(deltas, valueDelta{bit, data})
	}
	titlesChanged := false
	for i := 0; i < 6; i++ {
		if _, ok := changed[legacyPlayerKnownTitles+i]; ok {
			titlesChanged = true
		}
	}
	if titlesChanged {
		var payload []byte
		for i := 0; i < 3; i++ {
			field := legacyPlayerKnownTitles + i*2
			payload = binary.LittleEndian.AppendUint64(payload, uint64(values.field(field))|uint64(values.field(field+1))<<32)
		}
		deltas = append(deltas, valueDelta{3, payload})
	}
	for field, bit := range map[int]int{
		legacyPlayerHonorCurrency: 109, legacyPlayerAmmoID: 75, legacyPlayerPVPMedals: 76, legacyPlayerTodayContrib: 85,
		legacyPlayerLifetimeKills: 86, legacyPlayerYesterdayContrib: 89, legacyPlayerMaxLevel: 93, legacyPlayerPetSpellPower: 96,
	} {
		if _, ok := changed[field]; ok {
			add32(bit, field)
		}
	}
	if _, ok := changed[legacyPlayerKills]; ok {
		raw := values.field(legacyPlayerKills)
		deltas = append(deltas, valueDelta{77, binary.LittleEndian.AppendUint16(nil, uint16(raw))}, valueDelta{79, binary.LittleEndian.AppendUint16(nil, uint16(raw>>16))})
	}
	for index := 0; index < 64; index++ {
		field := legacyPlayerExplored1 + index*2
		_, lo := changed[field]
		_, hi := changed[field+1]
		if lo || hi {
			deltas = append(deltas, valueDelta{299 + index, binary.LittleEndian.AppendUint64(nil, uint64(values.field(field))|uint64(values.field(field+1))<<32)})
		}
	}
	_, restChanged := changed[legacyPlayerRestXP]
	_, restStateChanged := changed[legacyPlayerBytes2]
	if restChanged || restStateChanged {
		var mask uint32 = 1
		if restChanged {
			mask |= 2
		}
		if restStateChanged {
			mask |= 4
		}
		bits := newBitWriter(nil)
		bits.writeBits(mask, 3)
		payload := bits.flush()
		if restChanged {
			payload = binary.LittleEndian.AppendUint32(payload, values.field(legacyPlayerRestXP))
		}
		if restStateChanged {
			payload = append(payload, byte(values.field(legacyPlayerBytes2)>>24))
		}
		deltas = append(deltas, valueDelta{540, payload})
	}
	if _, ok := changed[legacyPlayerCoinage]; ok {
		deltas = append(deltas, valueDelta{28, binary.LittleEndian.AppendUint64(nil, uint64(values.field(legacyPlayerCoinage)))})
	}
	_, farsightLowChanged := changed[legacyPlayerFarsight]
	_, farsightHighChanged := changed[legacyPlayerFarsight+1]
	if farsightLowChanged || farsightHighChanged {
		// CAMERA follows PLAYER_FARSIGHT. Possession (Eye of Acherus) sets this
		// after ActivePlayer create, so CREATE-only encoding leaves the view on
		// the death knight. Bit 26 lives in the same parent-0 group as Coinage.
		deltas = append(deltas, valueDelta{26, values.appendLegacyGUID(nil, legacyPlayerFarsight, mapID)})
	}
	for field, bit := range map[int]int{
		legacyPlayerXP: 29, legacyPlayerNextLevelXP: 30,
		legacyPlayerCharacterPts1: 33, legacyPlayerCharacterPts2: 34,
		legacyPlayerTrackCreatures: 35,
	} {
		if _, ok := changed[field]; ok {
			add32(bit, field)
		}
	}
	// Watching a faction updates PLAYER_FIELD_WATCHED_FACTION_INDEX (1230).
	// AzerothCore echoes the new index with a single Values field, but without a
	// translation here the local player's modern WatchedFactionIndex descriptor
	// (bit 92, parent 70) never updates mid-session, so the portrait rep bar only
	// appears after a relog re-sends the CREATE.
	if _, ok := changed[legacyPlayerWatchedFaction]; ok {
		add32(92, legacyPlayerWatchedFaction)
	}
	for field, bit := range map[int]int{
		legacyPlayerBlockPercent: 41, legacyPlayerDodgePercent: 42,
		legacyPlayerParryPercent: 44, legacyPlayerCritPercent: 46,
		legacyPlayerRangedCrit: 47, legacyPlayerOffhandCrit: 48,
		legacyPlayerShieldBlock: 49,
		legacyPlayerHealingPos:  59, legacyPlayerHealingPct: 60,
		legacyPlayerHealingDonePct: 61, legacyPlayerTargetResist: 67,
		legacyPlayerTargetPhysical: 68,
	} {
		if _, ok := changed[field]; ok {
			add32(bit, field)
		}
	}
	if _, ok := changed[legacyPlayerExpertise]; ok {
		addExpertise(36, legacyPlayerExpertise)
	}
	if _, ok := changed[legacyPlayerOffExpertise]; ok {
		addExpertise(37, legacyPlayerOffExpertise)
	}
	if _, ok := changed[legacyPlayerTrackResources]; ok {
		add32(267, legacyPlayerTrackResources)
	}
	for school := 0; school < 7; school++ {
		for _, mapping := range []struct {
			field int
			bit   int
		}{
			{legacyPlayerSpellCrit1 + school, 270 + school},
			{legacyPlayerDamagePos1 + school, 277 + school},
			{legacyPlayerDamageNeg1 + school, 284 + school},
			{legacyPlayerDamagePct1 + school, 291 + school},
		} {
			if _, ok := changed[mapping.field]; ok {
				add32(mapping.bit, mapping.field)
			}
		}
	}
	// Build 54261 exposes 32 rating slots, while this WotLK create mapping
	// carries the first 20. Keep VALUES updates consistent with that layout.
	for index := 0; index < 20; index++ {
		field := legacyPlayerCombatRating1 + index
		if _, ok := changed[field]; ok {
			add32(575+index, field)
		}
	}
	// SPELL_AURA_NO_REAGENT_USE writes PLAYER_NO_REAGENT_COST_1..3. The 3.4.3
	// client skips spell reagents when this mask overlaps the spell class mask.
	// Element 619 has no WotLK source and stays unset.
	for index := 0; index < 3; index++ {
		field := legacyPlayerNoReagentCost1 + index
		if _, ok := changed[field]; ok {
			add32(616+index, field)
		}
	}
	if skill := encodeSkillInfoDelta(values, changed); len(skill) != 0 {
		deltas = append(deltas, valueDelta{bit: 32, data: skill})
	}
	// Build 54261: GlyphsEnabled is a byte; GlyphSlots and Glyphs share
	// parent 1512. Values payloads interleave the two arrays per slot (WPP343).
	if _, ok := changed[legacyPlayerGlyphsEnabled]; ok {
		deltas = append(deltas, valueDelta{120, []byte{byte(values.field(legacyPlayerGlyphsEnabled))}})
	}
	for slot := 0; slot < 6; slot++ {
		if _, ok := changed[legacyPlayerGlyphSlot1+slot]; ok {
			add32(1513+slot, legacyPlayerGlyphSlot1+slot)
		}
		if _, ok := changed[legacyPlayerGlyph1+slot]; ok {
			add32(1519+slot, legacyPlayerGlyph1+slot)
		}
	}
	// PLAYER_FIELD_BYTES[0] drives the local death UI. In particular, the
	// client does not send CMSG_REPOP_REQUEST until RELEASE_TIMER reaches its
	// ActivePlayerData.LocalFlags field. Re-send LocalFlags when the legacy
	// byte or player flags change, and when unit flags indicate a ghost. Entering
	// ghost state replaces the release timer with NO_RELEASE_WINDOW; leaving
	// it must clear NO_RELEASE_WINDOW even if AzerothCore leaves
	// PLAYER_FIELD_BYTES unchanged. Only bit 69 is needed here, and it must go
	// through modernPlayerLocalFlags: forwarding the raw legacy release-timer
	// bit after the ghost transition is what caused the old Disconnect 7.
	_, localFlagsChanged := changed[legacyPlayerFieldBytes]
	_, playerFlagsChanged := changed[legacyPlayerFlags]
	_, unitFlagsChanged := changed[legacyUnitFlags]
	if localFlagsChanged {
		raw := values.field(legacyPlayerFieldBytes)
		for i, bit := range []int{71, 72, 73} {
			deltas = append(deltas, valueDelta{bit, []byte{byte(raw >> uint(8*(i+1)))}})
		}
	}
	if localFlagsChanged || playerFlagsChanged || values.isLegacyGhost() && unitFlagsChanged {
		localFlags := modernPlayerLocalFlags(values.field(legacyPlayerFieldBytes), values.isLegacyGhost())
		deltas = append(deltas, valueDelta{69, binary.LittleEndian.AppendUint32(nil, localFlags)})
	}
	// PLAYER_FIELD_BYTES2 packs OverrideSpellsID (uint16 at bytes 0-1) with
	// AURA_VISION stealth (byte 3). AuraVision stays CREATE-only: a stealth
	// Values blob that also carries this ActivePlayer section is dropped
	// whole. OverrideSpellsID is the Extra Action set id and is safe to emit
	// when byte 0 actually changes (frenzy 0x00F1) or clears, never when the
	// stealth bit is the only meaningful write.
	if raw, ok := changed[legacyPlayerFieldBytes2]; ok && emitOverrideSpellsIDDelta(raw) {
		var id uint32
		if byte(raw) != 0 {
			id = legacyOverrideSpellsID(raw)
		}
		deltas = append(deltas, valueDelta{playerOverrideSpellsBit, binary.LittleEndian.AppendUint32(nil, id)})
	}
	for modernIndex := 0; modernIndex < 141; modernIndex++ {
		legacyField := modernInventoryLegacyField(modernIndex)
		if legacyField < 0 {
			continue
		}
		_, lowChanged := changed[legacyField]
		_, highChanged := changed[legacyField+1]
		if !lowChanged && !highChanged {
			continue
		}
		payload := values.appendLegacyGUID(nil, legacyField, mapID)
		deltas = append(deltas, valueDelta{125 + modernIndex, payload})
	}
	for slot := 0; slot < 12; slot++ {
		priceField := legacyPlayerBuybackPrice1 + slot
		if _, ok := changed[priceField]; ok {
			deltas = append(deltas, valueDelta{550 + slot, binary.LittleEndian.AppendUint32(nil, values.field(priceField))})
		}
		timeField := legacyPlayerBuybackTime1 + slot
		if _, ok := changed[timeField]; ok {
			deltas = append(deltas, valueDelta{562 + slot, binary.LittleEndian.AppendUint64(nil, uint64(values.field(timeField)))})
		}
	}
	return encodeActivePlayerFieldDeltas(deltas)
}

// encodeSkillInfoDelta maps the 128 packed WotLK PLAYER_SKILL_INFO records to
// build 54261's nested SkillInfo HasChangesMask<1793>. Each legacy uint32
// updates two adjacent uint16 fields, so both corresponding modern bits are
// marked and written even when only one half changed.
func encodeSkillInfoDelta(values legacyActivePlayerValues, changed map[int]uint32) []byte {
	blocks := make([]uint32, 57)
	set := func(bit int) { setMaskBit(blocks, bit) }
	changedSlot := false
	for slot := 0; slot < 128; slot++ {
		base := legacyPlayerSkill1 + slot*3
		if _, ok := changed[base]; ok {
			set(1 + slot)   // SkillLineID
			set(257 + slot) // SkillStep
			changedSlot = true
		}
		if _, ok := changed[base+1]; ok {
			set(513 + slot)  // SkillRank
			set(1025 + slot) // SkillMaxRank
			changedSlot = true
		}
		if _, ok := changed[base+2]; ok {
			set(1281 + slot) // SkillTempBonus
			set(1537 + slot) // SkillPermBonus
			changedSlot = true
		}
	}
	if !changedSlot {
		return nil
	}
	set(0)
	var mask0, mask1 uint32
	for index, block := range blocks {
		if block == 0 {
			continue
		}
		if index < 32 {
			mask0 |= 1 << index
		} else {
			mask1 |= 1 << (index - 32)
		}
	}
	dst := binary.LittleEndian.AppendUint32(nil, mask0)
	bits := newBitWriter(dst)
	bits.writeBits(mask1, 25)
	for _, block := range blocks {
		if block != 0 {
			bits.writeBits(block, 32)
		}
	}
	dst = bits.flush()
	for slot := 0; slot < 128; slot++ {
		base := legacyPlayerSkill1 + slot*3
		word0 := values.field(base)
		word1 := values.field(base + 1)
		word2 := values.field(base + 2)
		for _, field := range []struct {
			bit   int
			value uint16
		}{
			{1 + slot, uint16(word0)},
			{257 + slot, uint16(word0 >> 16)},
			{513 + slot, uint16(word1)},
			{769 + slot, 0},
			{1025 + slot, uint16(word1 >> 16)},
			{1281 + slot, uint16(word2)},
			{1537 + slot, uint16(word2 >> 16)},
		} {
			if blocks[field.bit/32]&(1<<uint(field.bit%32)) != 0 {
				dst = binary.LittleEndian.AppendUint16(dst, field.value)
			}
		}
	}
	return dst
}

func encodeActivePlayerFieldDeltas(deltas []valueDelta) []byte {
	if len(deltas) == 0 {
		return nil
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].bit < deltas[j].bit })
	blocks := make([]uint32, 48)
	for _, delta := range deltas {
		setMaskBit(blocks, delta.bit)
		if delta.bit == 3 || delta.bit >= 26 && delta.bit <= 37 {
			setMaskBit(blocks, 0)
		}
		if delta.bit >= 39 && delta.bit <= 69 {
			setMaskBit(blocks, 38)
		}
		if delta.bit >= 125 && delta.bit <= 265 {
			setMaskBit(blocks, 124)
		}
		if delta.bit >= 267 && delta.bit <= 268 {
			setMaskBit(blocks, 266)
		}
		if delta.bit >= 270 && delta.bit <= 297 {
			setMaskBit(blocks, 269)
		}
		if delta.bit >= 299 && delta.bit <= 538 {
			setMaskBit(blocks, 298)
		}
		if delta.bit >= 540 && delta.bit <= 541 {
			setMaskBit(blocks, 539)
		}
		if delta.bit >= 550 && delta.bit <= 573 {
			setMaskBit(blocks, 549)
		}
		if delta.bit >= 575 && delta.bit <= 606 {
			setMaskBit(blocks, 574)
		}
		if delta.bit >= 616 && delta.bit <= 618 {
			setMaskBit(blocks, 615)
		}
		if delta.bit >= 71 && delta.bit <= 101 {
			setMaskBit(blocks, 70)
		}
		if delta.bit >= 103 && delta.bit <= 123 {
			setMaskBit(blocks, playerAuraVisionParentBit)
		}
		if delta.bit >= 637 && delta.bit < 637+QuestCompletedBlockCount {
			setMaskBit(blocks, 636)
		}
		if delta.bit >= 1513 && delta.bit <= 1524 {
			setMaskBit(blocks, 1512)
		}
	}
	var mask0 uint32
	for index := 0; index < 32; index++ {
		if blocks[index] != 0 {
			mask0 |= 1 << index
		}
	}
	var mask1 uint32
	for index := 32; index < 48; index++ {
		if blocks[index] != 0 {
			mask1 |= 1 << (index - 32)
		}
	}
	dst := binary.LittleEndian.AppendUint32(nil, mask0)
	bits := newBitWriter(dst)
	bits.writeBits(mask1, 16)
	for _, block := range blocks {
		if block != 0 {
			bits.writeBits(block, 32)
		}
	}
	if blocks[0]&(1<<3) != 0 {
		bits.writeBits(3, 32)
		for i := 0; i < 3; i++ {
			bits.writeBit(true)
		}
	}
	dst = bits.flush()
	// The client reads HasPetStable whenever group 102 is present, even
	// when only GlyphsEnabled changed. It follows the scalar fields and
	// precedes the arrays. No stable payload is sent, so write false with
	// byte padding; omitting it shifts subsequent fields (Disconnect 7).
	sort.Slice(deltas, func(i, j int) bool { return activeDeltaOrder(deltas[i].bit) < activeDeltaOrder(deltas[j].bit) })
	stablePresencePending := blocks[102/32]&(1<<uint(102%32)) != 0
	for _, delta := range deltas {
		if stablePresencePending && delta.bit >= 124 {
			dst = append(dst, 0)
			stablePresencePending = false
		}
		dst = append(dst, delta.data...)
	}
	if stablePresencePending {
		dst = append(dst, 0)
	}
	return dst
}

func encodeItemValuesDelta(values legacyActivePlayerValues, changed map[int]uint32, mapID uint16) []byte {
	deltas := make([]valueDelta, 0, 32)
	for index, bit := range map[int]int{legacyItemOwner: 3, legacyItemContainedIn: 4, legacyItemCreator: 5, legacyItemGiftCreator: 6} {
		_, lowChanged := changed[index]
		_, highChanged := changed[index+1]
		if lowChanged || highChanged {
			deltas = append(deltas, valueDelta{bit, values.appendLegacyGUID(nil, index, mapID)})
		}
	}
	for field, bit := range map[int]int{
		legacyItemStackCount: 7, legacyItemDuration: 8, legacyItemFlags: 9,
		legacyItemPropertySeed: 10, legacyItemRandom: 11, legacyItemDurability: 12,
		legacyItemMaxDurability: 13, legacyItemCreatePlayed: 14,
	} {
		if _, ok := changed[field]; ok {
			deltas = append(deltas, valueDelta{bit, binary.LittleEndian.AppendUint32(nil, values.field(field))})
		}
	}
	for index := 0; index < 5; index++ {
		field := legacyItemSpellCharges + index
		if _, ok := changed[field]; ok {
			deltas = append(deltas, valueDelta{24 + index, binary.LittleEndian.AppendUint32(nil, values.field(field))})
		}
	}
	for slot := 0; slot < 12; slot++ {
		base := legacyItemEnchantment + slot*3
		var innerMask uint32
		if _, ok := changed[base]; ok {
			innerMask |= 2
		}
		if _, ok := changed[base+1]; ok {
			innerMask |= 4
		}
		if _, ok := changed[base+2]; ok {
			innerMask |= 8
		}
		if innerMask == 0 {
			continue
		}
		innerMask |= 1
		// legacy proxy WriteUpdateItemData writes ItemEnchantment as HasChangesMask<6>
		// (WriteMSBits 6 + FlushBits): hasAny, ID, Duration, Charges, plus two
		// extra byte fields that 3.3.5 never changes. Hermes 3.4.3 / the old
		// redscarf path used 4 bits; the client then reads those 4 bits plus
		// the flush padding as a 6-bit mask (0x0F -> 0x3C), clears hasAny, and
		// drops the session (reason 7) on a sharpening-stone apply.
		bits := newBitWriter(nil)
		bits.writeBits(innerMask, 6)
		payload := bits.flush()
		if innerMask&2 != 0 {
			payload = binary.LittleEndian.AppendUint32(payload, values.field(base))
		}
		if innerMask&4 != 0 {
			payload = binary.LittleEndian.AppendUint32(payload, values.field(base+1))
		}
		if innerMask&8 != 0 {
			payload = binary.LittleEndian.AppendUint16(payload, uint16(values.field(base+2)))
		}
		modernSlot := slot
		if slot >= 7 {
			modernSlot++ // build 54261 inserts Use before the random enchantments
		}
		deltas = append(deltas, valueDelta{30 + modernSlot, payload})
	}
	gemsChanged := itemSocketGemsChanged(changed)
	var gems []uint32
	if gemsChanged {
		gems = itemSocketedGems(values)
		deltas = append(deltas, valueDelta{itemDataGemsBit, encodeSocketedGemsUpdate(gems)})
	}
	return encodeBlockDeltas(deltas, 2, 2, false, func(bits *bitWriter, blocks []uint32) {
		if bits == nil {
			for _, delta := range deltas {
				switch {
				case delta.bit >= 1 && delta.bit <= 18:
					setMaskBit(blocks, 0)
				case delta.bit >= 24 && delta.bit <= 28:
					setMaskBit(blocks, 23)
				case delta.bit >= 30 && delta.bit <= 42:
					setMaskBit(blocks, 29)
				}
			}
			return
		}
		if gemsChanged {
			writeItemGemsUpdateMask(bits, gems)
		}
	})
}

func encodeContainerValuesDelta(values legacyActivePlayerValues, changed map[int]uint32, mapID uint16) []byte {
	deltas := make([]valueDelta, 0, 37)
	if _, ok := changed[legacyContainerNumSlots]; ok {
		deltas = append(deltas, valueDelta{1, binary.LittleEndian.AppendUint32(nil, values.field(legacyContainerNumSlots))})
	}
	for slot := 0; slot < 36; slot++ {
		field := legacyContainerSlot1 + slot*2
		_, lowChanged := changed[field]
		_, highChanged := changed[field+1]
		if lowChanged || highChanged {
			deltas = append(deltas, valueDelta{3 + slot, values.appendLegacyGUID(nil, field, mapID)})
		}
	}
	return encodeBlockDeltas(deltas, 2, 2, false, func(_ *bitWriter, blocks []uint32) {
		for _, delta := range deltas {
			if delta.bit == 1 {
				setMaskBit(blocks, 0)
			} else {
				setMaskBit(blocks, 2)
			}
		}
	})
}

func encodeGameObjectValuesDelta(values legacyActivePlayerValues, changed map[int]uint32, mapID uint16) []byte {
	deltas := make([]valueDelta, 0, 16)
	add32 := func(bit, field int) {
		deltas = append(deltas, valueDelta{bit, binary.LittleEndian.AppendUint32(nil, values.field(field))})
	}
	for field, bit := range map[int]int{
		legacyGameObjectDisplayID: 4, legacyGameObjectFlags: 11,
		legacyGameObjectFaction: 13, legacyGameObjectLevel: 14,
	} {
		if _, ok := changed[field]; ok {
			add32(bit, field)
		}
	}
	_, creatorLow := changed[legacyGameObjectCreatedBy]
	_, creatorHigh := changed[legacyGameObjectCreatedBy+1]
	if creatorLow || creatorHigh {
		deltas = append(deltas, valueDelta{9, values.appendLegacyGUID(nil, legacyGameObjectCreatedBy, mapID)})
	}
	rotationChanged := false
	for index := 0; index < 4; index++ {
		if _, ok := changed[legacyGameObjectRotation+index]; ok {
			rotationChanged = true
		}
	}
	if rotationChanged {
		payload := make([]byte, 0, 16)
		for index := 0; index < 4; index++ {
			fallback := uint32(0)
			if index == 3 {
				fallback = math.Float32bits(1)
			}
			payload = binary.LittleEndian.AppendUint32(payload, values.fieldOr(legacyGameObjectRotation+index, fallback))
		}
		deltas = append(deltas, valueDelta{12, payload})
	}
	if _, ok := changed[legacyGameObjectBytes1]; ok {
		raw := values.field(legacyGameObjectBytes1)
		deltas = append(deltas,
			valueDelta{15, []byte{byte(raw)}},
			valueDelta{16, []byte{byte(raw >> 8)}},
			valueDelta{17, []byte{byte(raw >> 24)}},
			valueDelta{18, binary.LittleEndian.AppendUint32(nil, uint32(byte(raw>>16)))},
		)
	}
	if len(deltas) == 0 {
		return nil
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].bit < deltas[j].bit })
	var mask uint32 = 1
	for _, delta := range deltas {
		mask |= 1 << delta.bit
	}
	bits := newBitWriter(nil)
	bits.writeBits(mask, 20)
	dst := bits.flush()
	for _, delta := range deltas {
		dst = append(dst, delta.data...)
	}
	return dst
}

func encodeBlockDeltas(deltas []valueDelta, blockCount, prefixBits int, rootFirstFour bool, mutate func(*bitWriter, []uint32)) []byte {
	if len(deltas) == 0 {
		return nil
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].bit < deltas[j].bit })
	blocks := make([]uint32, blockCount)
	for _, delta := range deltas {
		setMaskBit(blocks, delta.bit)
	}
	if mutate != nil {
		mutate(nil, blocks)
	}
	if rootFirstFour {
		for index := 0; index < 4 && index < len(blocks); index++ {
			if blocks[index] != 0 {
				blocks[index] |= 1
			}
		}
	}
	var blocksMask uint32
	for index, block := range blocks {
		if block != 0 {
			blocksMask |= 1 << index
		}
	}
	bits := newBitWriter(nil)
	bits.writeBits(blocksMask, prefixBits)
	for _, block := range blocks {
		if block != 0 {
			bits.writeBits(block, 32)
		}
	}
	if mutate != nil {
		mutate(bits, blocks)
	}
	dst := bits.flush()
	for _, delta := range deltas {
		dst = append(dst, delta.data...)
	}
	return dst
}

func setMaskBit(blocks []uint32, bit int) {
	if bit < 0 || bit/32 >= len(blocks) {
		return
	}
	blocks[bit/32] |= 1 << (bit % 32)
}
