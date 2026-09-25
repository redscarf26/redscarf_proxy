package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	SMSGGameObjectCustomAnim       = uint16(0x25C4)
	SMSGGameObjectDespawn          = uint16(0x25C5)
	SMSGFishNotHooked              = uint16(0x26CF)
	SMSGFishEscaped                = uint16(0x26D0)
	SMSGGameObjectResetState       = uint16(0x271E)
	SMSGAllAchievementData         = uint16(0x2570)
	SMSGRespondInspectAchievements = uint16(0x2572)
	SMSGAchievementEarned          = uint16(0x2643)
	SMSGAchievementDeleted         = uint16(0x26E0)
	SMSGCriteriaUpdate             = uint16(0x26E1)
	SMSGStartMirrorTimer           = uint16(0x270F)
	SMSGPauseMirrorTimer           = uint16(0x2710)
	SMSGStopMirrorTimer            = uint16(0x2711)
	SMSGSetFactionStanding         = uint16(0x272C)
	SMSGLootList                   = uint16(0x2741)
	SMSGExplorationExperience      = uint16(0x275F)
	SMSGPlaySound                  = uint16(0x276C)
	SMSGPlayMusic                  = uint16(0x276D)
	SMSGZoneUnderAttack            = uint16(0x2BB5)
	SMSGInvalidatePlayer           = uint16(0x2FFF)
	SMSGSpecialMountAnim           = uint16(0x269F)
	SMSGDismount                   = uint16(0x26B1)
)

const CMSGQueryInspectAchievements = uint16(0x3500)

type MirrorTimer struct {
	Timer   int32
	Value   int32
	Maximum int32
	Scale   int32
	SpellID int32
	Paused  bool
}

type ExplorationExperience struct {
	AreaID     uint32
	Experience uint32
}

func ParseLegacyExplorationExperience(body []byte) (ExplorationExperience, error) {
	if len(body) != 8 {
		return ExplorationExperience{}, fmt.Errorf("exploration-experience has %d bytes, want 8", len(body))
	}
	return ExplorationExperience{
		AreaID:     binary.LittleEndian.Uint32(body[0:4]),
		Experience: binary.LittleEndian.Uint32(body[4:8]),
	}, nil
}

func EncodeExplorationExperience(experience ExplorationExperience) []byte {
	body := binary.LittleEndian.AppendUint32(nil, experience.AreaID)
	return binary.LittleEndian.AppendUint32(body, experience.Experience)
}

func ParseLegacyDismount(body []byte) (uint64, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, fmt.Errorf("read dismount GUID: %w", err)
	}
	if r.remaining() != 0 {
		return 0, fmt.Errorf("dismount has %d trailing bytes", r.remaining())
	}
	return guid, nil
}

func EncodeDismount(guid GUID128) []byte {
	return appendPackedGUID128(nil, guid.Low, guid.High)
}

func ParseLegacyStartMirrorTimer(body []byte) (MirrorTimer, error) {
	var timer MirrorTimer
	if len(body) != 21 {
		return timer, fmt.Errorf("start-mirror-timer has %d bytes, want 21", len(body))
	}
	timer.Timer = int32(binary.LittleEndian.Uint32(body[0:4]))
	timer.Value = int32(binary.LittleEndian.Uint32(body[4:8]))
	timer.Maximum = int32(binary.LittleEndian.Uint32(body[8:12]))
	timer.Scale = int32(binary.LittleEndian.Uint32(body[12:16]))
	if body[16] > 1 {
		return timer, fmt.Errorf("start-mirror-timer has invalid paused flag %d", body[16])
	}
	timer.Paused = body[16] != 0
	timer.SpellID = int32(binary.LittleEndian.Uint32(body[17:21]))
	return timer, nil
}

func EncodeStartMirrorTimer(timer MirrorTimer) []byte {
	var body []byte
	body = binary.LittleEndian.AppendUint32(body, uint32(timer.Timer))
	body = binary.LittleEndian.AppendUint32(body, uint32(timer.Value))
	body = binary.LittleEndian.AppendUint32(body, uint32(timer.Maximum))
	body = binary.LittleEndian.AppendUint32(body, uint32(timer.Scale))
	body = binary.LittleEndian.AppendUint32(body, uint32(timer.SpellID))
	bits := newBitWriter(body)
	bits.writeBit(timer.Paused)
	return bits.flush()
}

func ParseLegacyStopMirrorTimer(body []byte) (int32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("stop-mirror-timer has %d bytes, want 4", len(body))
	}
	return int32(binary.LittleEndian.Uint32(body)), nil
}

func EncodeStopMirrorTimer(timer int32) []byte {
	return binary.LittleEndian.AppendUint32(nil, uint32(timer))
}

// ParseLegacyPauseMirrorTimer reads the 3.3.5a pause notice: a timer id and a
// 0/1 paused byte. Build 54261 carries the same id, then the paused state as one bit.
func ParseLegacyPauseMirrorTimer(body []byte) (int32, bool, error) {
	if len(body) != 5 {
		return 0, false, fmt.Errorf("pause-mirror-timer has %d bytes, want 5", len(body))
	}
	if body[4] > 1 {
		return 0, false, fmt.Errorf("pause-mirror-timer has invalid paused flag %d", body[4])
	}
	return int32(binary.LittleEndian.Uint32(body[:4])), body[4] != 0, nil
}

func EncodePauseMirrorTimer(timer int32, paused bool) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(timer))
	bits := newBitWriter(body)
	bits.writeBit(paused)
	return bits.flush()
}

// ParseLegacyPlayMusic reads the 4-byte sound id. Unlike SMSG_PLAY_SOUND, the
// 54261 music packet has no source GUID.
func ParseLegacyPlayMusic(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("play-music has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func EncodePlayMusic(soundID uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, soundID)
}

func ParseLegacyZoneUnderAttack(body []byte) (int32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("zone-under-attack has %d bytes, want 4", len(body))
	}
	return int32(binary.LittleEndian.Uint32(body)), nil
}

func EncodeZoneUnderAttack(areaID int32) []byte {
	return binary.LittleEndian.AppendUint32(nil, uint32(areaID))
}

func ParseLegacyInvalidatePlayer(body []byte) (uint64, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, fmt.Errorf("read invalidate-player GUID: %w", err)
	}
	if r.remaining() != 0 {
		return 0, fmt.Errorf("invalidate-player has %d trailing bytes", r.remaining())
	}
	return guid, nil
}

func EncodeInvalidatePlayer(guid GUID128) []byte {
	return appendPackedGUID128(nil, guid.Low, guid.High)
}

func ParseLegacySpecialMountAnim(body []byte) (uint64, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, fmt.Errorf("read special-mount-anim GUID: %w", err)
	}
	if r.remaining() != 0 {
		return 0, fmt.Errorf("special-mount-anim has %d trailing bytes", r.remaining())
	}
	return guid, nil
}

// EncodeSpecialMountAnim writes the 54261 mount packet with an empty visual-kit
// list. The legacy packet only carries a GUID, and neither Hermes nor legacy proxy
// invents SpellVisualKitIDs for it.
func EncodeSpecialMountAnim(guid GUID128) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint32(body, 0) // SpellVisualKitIDs count
	return binary.LittleEndian.AppendUint32(body, 0) // SequenceVariation
}

func ParseLegacyPlaySound(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("play-sound has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func EncodePlaySound(soundKitID uint32, source GUID128) []byte {
	body := binary.LittleEndian.AppendUint32(nil, soundKitID)
	body = appendPackedGUID128(body, source.Low, source.High)
	return binary.LittleEndian.AppendUint32(body, 0) // BroadcastTextID
}

func ParseLegacyUnpackedGUID(body []byte) (uint64, error) {
	if len(body) != 8 {
		return 0, fmt.Errorf("unpacked GUID has %d bytes, want 8", len(body))
	}
	return binary.LittleEndian.Uint64(body), nil
}

func EncodeGameObjectDespawn(guid GUID128) []byte {
	return appendPackedGUID128(nil, guid.Low, guid.High)
}

type LegacyGameObjectCustomAnim struct {
	GUID       uint64
	CustomAnim uint32
}

func ParseLegacyGameObjectCustomAnim(body []byte) (LegacyGameObjectCustomAnim, error) {
	var anim LegacyGameObjectCustomAnim
	if len(body) != 12 {
		return anim, fmt.Errorf("game-object-custom-anim has %d bytes, want 12", len(body))
	}
	anim.GUID = binary.LittleEndian.Uint64(body[:8])
	anim.CustomAnim = binary.LittleEndian.Uint32(body[8:])
	return anim, nil
}

func EncodeGameObjectCustomAnim(guid GUID128, customAnim uint32) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint32(body, customAnim)
	bits := newBitWriter(body)
	bits.writeBit(false) // PlayAsDespawn does not exist in the legacy packet.
	return bits.flush()
}

func EncodeGameObjectResetState(guid GUID128) []byte {
	return appendPackedGUID128(nil, guid.Low, guid.High)
}

func ValidateLegacyFishEvent(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("fish event has %d bytes, want 0", len(body))
	}
	return nil
}

type LegacyLootList struct {
	Owner            uint64
	Master           uint64
	RoundRobinWinner uint64
}

func ParseLegacyLootList(body []byte) (LegacyLootList, error) {
	var list LegacyLootList
	r := movementReader{data: body}
	var err error
	if list.Owner, err = r.u64(); err != nil {
		return list, fmt.Errorf("read loot owner: %w", err)
	}
	if list.Master, err = r.guid64(); err != nil {
		return list, fmt.Errorf("read master looter: %w", err)
	}
	if list.RoundRobinWinner, err = r.guid64(); err != nil {
		return list, fmt.Errorf("read round-robin winner: %w", err)
	}
	if r.remaining() != 0 {
		return list, fmt.Errorf("loot-list has %d trailing bytes", r.remaining())
	}
	return list, nil
}

func EncodeLootList(owner, lootObject, master, roundRobinWinner GUID128) []byte {
	body := appendPackedGUID128(nil, owner.Low, owner.High)
	body = appendPackedGUID128(body, lootObject.Low, lootObject.High)
	bits := newBitWriter(body)
	bits.writeBit(master.Low != 0 || master.High != 0)
	bits.writeBit(roundRobinWinner.Low != 0 || roundRobinWinner.High != 0)
	body = bits.flush()
	if master.Low != 0 || master.High != 0 {
		body = appendPackedGUID128(body, master.Low, master.High)
	}
	if roundRobinWinner.Low != 0 || roundRobinWinner.High != 0 {
		body = appendPackedGUID128(body, roundRobinWinner.Low, roundRobinWinner.High)
	}
	return body
}

type FactionStanding struct {
	Index    uint32
	Standing int32
}

type LegacyFactionStandingUpdate struct {
	Bonus      float32
	ShowVisual bool
	Factions   []FactionStanding
}

func ParseLegacyFactionStanding(body []byte) (LegacyFactionStandingUpdate, error) {
	var update LegacyFactionStandingUpdate
	if len(body) < 9 {
		return update, fmt.Errorf("faction-standing has %d bytes, want at least 9", len(body))
	}
	update.Bonus = math.Float32frombits(binary.LittleEndian.Uint32(body[0:4]))
	if body[4] > 1 {
		return update, fmt.Errorf("faction-standing has invalid visual flag %d", body[4])
	}
	update.ShowVisual = body[4] != 0
	count := int(binary.LittleEndian.Uint32(body[5:9]))
	if count > 256 || len(body) != 9+count*8 {
		return update, fmt.Errorf("faction-standing count %d does not match %d-byte body", count, len(body))
	}
	update.Factions = make([]FactionStanding, count)
	position := 9
	for index := range update.Factions {
		update.Factions[index].Index = binary.LittleEndian.Uint32(body[position : position+4])
		update.Factions[index].Standing = int32(binary.LittleEndian.Uint32(body[position+4 : position+8]))
		position += 8
	}
	return update, nil
}

func EncodeFactionStanding(update LegacyFactionStandingUpdate) []byte {
	// Build 54261 SMSG_SET_FACTION_STANDING carries two bonus floats before the
	// count: ReferAFriendBonus (the legacy bonus passes through) followed by
	// BonusFromAchievementSystem, which 3.3.5a never sets. Mirror
	// HermesProxy SetFactionStanding.cs. Omitting the second float shifts every
	// subsequent field and makes the client mis-parse the standings update.
	body := binary.LittleEndian.AppendUint32(nil, math.Float32bits(update.Bonus))
	body = binary.LittleEndian.AppendUint32(body, 0) // BonusFromAchievementSystem
	body = binary.LittleEndian.AppendUint32(body, uint32(len(update.Factions)))
	for _, faction := range update.Factions {
		body = binary.LittleEndian.AppendUint32(body, faction.Index)
		body = binary.LittleEndian.AppendUint32(body, uint32(faction.Standing))
	}
	bits := newBitWriter(body)
	bits.writeBit(update.ShowVisual)
	return bits.flush()
}

type LegacyCriteriaUpdate struct {
	CriteriaID      uint32
	Quantity        uint64
	Player          uint64
	Flags           uint32
	CurrentTime     uint32
	ElapsedTime     uint32
	CreationElapsed uint32
}

func ParseLegacyCriteriaUpdate(body []byte) (LegacyCriteriaUpdate, error) {
	r := movementReader{data: body}
	update, err := parseLegacyCriteriaUpdate(&r)
	if err != nil {
		return update, err
	}
	if r.remaining() != 0 {
		return update, fmt.Errorf("criteria-update has %d trailing bytes", r.remaining())
	}
	return update, nil
}

func parseLegacyCriteriaUpdate(r *movementReader) (LegacyCriteriaUpdate, error) {
	var update LegacyCriteriaUpdate
	var err error
	if update.CriteriaID, err = r.u32(); err != nil {
		return update, fmt.Errorf("read criteria ID: %w", err)
	}
	if update.Quantity, err = r.guid64(); err != nil {
		return update, fmt.Errorf("read criteria quantity: %w", err)
	}
	if update.Player, err = r.guid64(); err != nil {
		return update, fmt.Errorf("read criteria player: %w", err)
	}
	if update.Flags, err = r.u32(); err != nil {
		return update, fmt.Errorf("read criteria flags: %w", err)
	}
	if update.CurrentTime, err = r.u32(); err != nil {
		return update, fmt.Errorf("read criteria time: %w", err)
	}
	if update.ElapsedTime, err = r.u32(); err != nil {
		return update, fmt.Errorf("read criteria elapsed time: %w", err)
	}
	if update.CreationElapsed, err = r.u32(); err != nil { // WotLK average/second elapsed field
		return update, fmt.Errorf("read criteria creation elapsed time: %w", err)
	}
	return update, nil
}

func EncodeCriteriaUpdate(update LegacyCriteriaUpdate, player GUID128) []byte {
	body := binary.LittleEndian.AppendUint32(nil, update.CriteriaID)
	body = binary.LittleEndian.AppendUint64(body, update.Quantity)
	body = appendPackedGUID128(body, player.Low, player.High)
	body = binary.LittleEndian.AppendUint32(body, 0) // Unused_10_1_5
	body = binary.LittleEndian.AppendUint32(body, update.Flags)
	body = binary.LittleEndian.AppendUint32(body, update.CurrentTime)
	body = binary.LittleEndian.AppendUint64(body, uint64(update.ElapsedTime))
	body = binary.LittleEndian.AppendUint64(body, uint64(update.CreationElapsed))
	bits := newBitWriter(body)
	bits.writeBit(false) // no RAF acceptance ID
	return bits.flush()
}

type LegacyEarnedAchievement struct {
	AchievementID uint32
	Date          uint32
}

type LegacyAchievementEarned struct {
	Player        uint64
	AchievementID uint32
	Date          uint32
}

func ParseLegacyAchievementEarned(body []byte) (LegacyAchievementEarned, error) {
	var earned LegacyAchievementEarned
	r := movementReader{data: body}
	var err error
	if earned.Player, err = r.guid64(); err != nil {
		return earned, fmt.Errorf("read achievement player: %w", err)
	}
	if earned.AchievementID, err = r.u32(); err != nil {
		return earned, fmt.Errorf("read achievement ID: %w", err)
	}
	if earned.Date, err = r.u32(); err != nil {
		return earned, fmt.Errorf("read achievement date: %w", err)
	}
	if _, err = r.u32(); err != nil { // legacy effect-skip placeholder
		return earned, fmt.Errorf("read achievement effect placeholder: %w", err)
	}
	if r.remaining() != 0 {
		return earned, fmt.Errorf("achievement-earned has %d trailing bytes", r.remaining())
	}
	return earned, nil
}

func EncodeAchievementEarned(earned LegacyAchievementEarned, player GUID128, realmAddress uint32) []byte {
	body := appendPackedGUID128(nil, player.Low, player.High) // Sender
	body = appendPackedGUID128(body, player.Low, player.High) // Earner
	body = binary.LittleEndian.AppendUint32(body, earned.AchievementID)
	body = binary.LittleEndian.AppendUint32(body, earned.Date)
	body = binary.LittleEndian.AppendUint32(body, realmAddress)
	body = binary.LittleEndian.AppendUint32(body, realmAddress)
	bits := newBitWriter(body)
	bits.writeBit(false) // Initial
	return bits.flush()
}

// ParseLegacyAchievementDeleted reads the 4-byte legacy SMSG_ACHIEVEMENT_DELETED,
// sent by AzerothCore only on achievement resets (e.g. .achievement remove).
func ParseLegacyAchievementDeleted(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("achievement-deleted has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func EncodeAchievementDeleted(achievementID uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, achievementID)
	return binary.LittleEndian.AppendUint32(body, 0) // Immunities
}

type LegacyAllAchievementData struct {
	Earned   []LegacyEarnedAchievement
	Progress []LegacyCriteriaUpdate
}

func ParseLegacyAllAchievementData(body []byte) (LegacyAllAchievementData, error) {
	var data LegacyAllAchievementData
	r := movementReader{data: body}
	for {
		achievementID, err := r.u32()
		if err != nil {
			return data, fmt.Errorf("read earned achievement ID: %w", err)
		}
		if achievementID == math.MaxUint32 {
			break
		}
		date, err := r.u32()
		if err != nil {
			return data, fmt.Errorf("read earned achievement %d date: %w", achievementID, err)
		}
		if len(data.Earned) >= 10000 {
			return data, fmt.Errorf("all-achievement-data has too many earned achievements")
		}
		data.Earned = append(data.Earned, LegacyEarnedAchievement{AchievementID: achievementID, Date: date})
	}
	for {
		if r.remaining() < 4 {
			return data, fmt.Errorf("all-achievement-data is missing progress terminator")
		}
		if binary.LittleEndian.Uint32(r.data[r.offset:r.offset+4]) == math.MaxUint32 {
			r.offset += 4
			break
		}
		if len(data.Progress) >= 50000 {
			return data, fmt.Errorf("all-achievement-data has too many criteria")
		}
		progress, err := parseLegacyCriteriaUpdate(&r)
		if err != nil {
			return data, fmt.Errorf("read criteria progress %d: %w", len(data.Progress), err)
		}
		data.Progress = append(data.Progress, progress)
	}
	if r.remaining() != 0 {
		return data, fmt.Errorf("all-achievement-data has %d trailing bytes", r.remaining())
	}
	return data, nil
}

func EncodeAllAchievementData(data LegacyAllAchievementData, owner GUID128, realmAddress uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(data.Earned)))
	body = binary.LittleEndian.AppendUint32(body, uint32(len(data.Progress)))
	for _, earned := range data.Earned {
		body = binary.LittleEndian.AppendUint32(body, earned.AchievementID)
		body = binary.LittleEndian.AppendUint32(body, earned.Date)
		body = appendPackedGUID128(body, owner.Low, owner.High)
		body = binary.LittleEndian.AppendUint32(body, realmAddress)
		body = binary.LittleEndian.AppendUint32(body, realmAddress)
	}
	for _, progress := range data.Progress {
		body = append(body, EncodeCriteriaUpdate(progress, owner)...)
	}
	return body
}

func ParseLegacyInspectAchievements(body []byte) (uint64, LegacyAllAchievementData, error) {
	r := movementReader{data: body}
	owner, err := r.guid64()
	if err != nil {
		return 0, LegacyAllAchievementData{}, fmt.Errorf("read inspected achievement owner: %w", err)
	}
	data, err := ParseLegacyAllAchievementData(r.data[r.offset:])
	if err != nil {
		return 0, LegacyAllAchievementData{}, err
	}
	return owner, data, nil
}

func EncodeRespondInspectAchievements(data LegacyAllAchievementData, owner GUID128, realmAddress uint32) []byte {
	body := appendPackedGUID128(nil, owner.Low, owner.High)
	return append(body, EncodeAllAchievementData(data, owner, realmAddress)...)
}
