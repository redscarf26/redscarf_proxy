package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	CMSGArenaTeamRoster             = uint16(0x36b8)
	CMSGArenaTeamAccept             = uint16(0x36b9)
	CMSGArenaTeamRemove             = uint16(0x36bc)
	CMSGArenaTeamDisband            = uint16(0x36bd)
	CMSGBattlemasterJoinArena       = uint16(0x3521)
	CMSGBattlemasterJoinSkirmish    = uint16(0x3522)
	SMSGArenaTeamRoster             = uint16(0x2760)
	SMSGArenaTeamInvite             = uint16(0x2761)
	SMSGArenaTeamEvent              = uint16(0x2762)
	SMSGArenaTeamCommandResult      = uint16(0x2763)
	SMSGQueryArenaTeamResponse      = uint16(0x2920)
	CMSGBattlePetRequestJournalLock = uint16(0x3624)

	// LegacyPlayerArenaTeamInfo is PLAYER_FIELD_ARENA_TEAM_INFO_1_1 on 3.3.5a.
	// Each of the three slots is ArenaTeamInfoStride uint32s (the block is 21),
	// and the team id is the first word. Hermes walks these with stride 6, which
	// reads the 3v3 and 5v5 ids from the wrong words.
	LegacyPlayerArenaTeamInfo = 1256
	ArenaTeamInfoStride       = 7
)

func ParseArenaTeamRoster(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("arena-team roster has %d bytes, want 4", len(body))
	}
	index := binary.LittleEndian.Uint32(body)
	if index > 2 {
		return 0, fmt.Errorf("arena-team roster index %d is invalid", index)
	}
	return index, nil
}

func EncodeEmptyArenaTeamRoster(index uint32) []byte {
	teamSize := uint32(0)
	switch index {
	case 0:
		teamSize = 2
	case 1:
		teamSize = 3
	case 2:
		teamSize = 5
	}
	body := binary.LittleEndian.AppendUint32(nil, 0) // TeamID
	body = binary.LittleEndian.AppendUint32(body, teamSize)
	for range 7 { // ratings/statistics and the empty member count
		body = binary.LittleEndian.AppendUint32(body, 0)
	}
	bits := newBitWriter(body)
	bits.writeBit(false) // UnkBit
	return bits.flush()
}

// ArenaTeamIDFromFields reads one slot's team id from a player's cached
// update fields. A missing word is an empty slot.
func ArenaTeamIDFromFields(fields map[int]uint32, slot uint32) uint32 {
	if fields == nil || slot >= ArenaSlotCount {
		return 0
	}
	return fields[LegacyPlayerArenaTeamInfo+int(slot)*ArenaTeamInfoStride]
}

// ArenaTeamStats is SMSG_ARENA_TEAM_STATS. The modern roster header carries
// these numbers; 54261 has no separate stats writer in legacy proxy.
type ArenaTeamStats struct {
	WeekPlayed   uint32
	WeekWins     uint32
	SeasonPlayed uint32
	SeasonWins   uint32
	Rating       uint32
	Rank         uint32
}

// ArenaTeamEmblem is the legacy query response: name, team size and banner.
type ArenaTeamEmblem struct {
	TeamID          uint32
	Name            string
	TeamSize        uint32
	BackgroundColor uint32
	EmblemStyle     uint32
	EmblemColor     uint32
	BorderStyle     uint32
	BorderColor     uint32
}

// LegacyArenaMember is one row of SMSG_ARENA_TEAM_ROSTER. Captain is 0 for
// the captain and 1 for everyone else. HiddenRating is set only when the
// legacy unk308 flag is non-zero; AzerothCore always sends 0.
type LegacyArenaMember struct {
	GUID           uint64
	Online         bool
	Name           string
	Captain        int32
	Level          uint8
	Class          uint8
	WeekGames      uint32
	WeekWins       uint32
	SeasonGames    uint32
	SeasonWins     uint32
	PersonalRating uint32
	HiddenRating   *[2]float32
}

// LegacyArenaTeamRoster is the legacy roster without the team statistics,
// which arrive in a separate stats packet.
type LegacyArenaTeamRoster struct {
	TeamID   uint32
	TeamSize uint32
	Members  []LegacyArenaMember
}

// ArenaTeamCommand is SMSG_ARENA_TEAM_COMMAND_RESULT. Action and Error are
// forwarded as their AzerothCore values; those numbers already match 54261.
type ArenaTeamCommand struct {
	Action     uint32
	TeamName   string
	PlayerName string
	Error      uint32
}

// ArenaTeamNotice is SMSG_ARENA_TEAM_EVENT after the legacy event byte has
// been increased by one (3..8 becomes 4..9).
type ArenaTeamNotice struct {
	Event  uint8
	Params [3]string
}

// ArenaTeamInviteNames is the inviter name and the team name from
// SMSG_ARENA_TEAM_INVITE. The legacy packet has no GUIDs.
type ArenaTeamInviteNames struct {
	PlayerName string
	TeamName   string
}

// BattlemasterJoin is a rated or skirmish arena queue request. Slot is the
// legacy 0/1/2 bracket.
type BattlemasterJoin struct {
	GUID    GUID128
	Slot    byte
	AsGroup bool
}

func ParseLegacyArenaTeamQuery(body []byte) (ArenaTeamEmblem, error) {
	var emblem ArenaTeamEmblem
	r := movementReader{data: body}
	var err error
	if emblem.TeamID, err = r.u32(); err != nil {
		return emblem, fmt.Errorf("read arena-team query id: %w", err)
	}
	if emblem.Name, err = r.cstring(); err != nil {
		return emblem, fmt.Errorf("read arena-team query name: %w", err)
	}
	if emblem.TeamSize, err = r.u32(); err != nil {
		return emblem, fmt.Errorf("read arena-team query size: %w", err)
	}
	colors := []*uint32{
		&emblem.BackgroundColor, &emblem.EmblemStyle, &emblem.EmblemColor,
		&emblem.BorderStyle, &emblem.BorderColor,
	}
	for _, color := range colors {
		if *color, err = r.u32(); err != nil {
			return emblem, fmt.Errorf("read arena-team query emblem: %w", err)
		}
	}
	if err = r.done(); err != nil {
		return emblem, fmt.Errorf("arena-team query: %w", err)
	}
	return emblem, nil
}

func ParseLegacyArenaTeamStats(body []byte) (uint32, ArenaTeamStats, error) {
	var stats ArenaTeamStats
	r := movementReader{data: body}
	teamID, err := r.u32()
	if err != nil {
		return 0, stats, fmt.Errorf("read arena-team stats id: %w", err)
	}
	fields := []*uint32{
		&stats.Rating, &stats.WeekPlayed, &stats.WeekWins,
		&stats.SeasonPlayed, &stats.SeasonWins, &stats.Rank,
	}
	for _, field := range fields {
		if *field, err = r.u32(); err != nil {
			return 0, stats, fmt.Errorf("read arena-team stats: %w", err)
		}
	}
	if err = r.done(); err != nil {
		return 0, stats, fmt.Errorf("arena-team stats: %w", err)
	}
	return teamID, stats, nil
}

func ParseLegacyArenaTeamRoster(body []byte) (LegacyArenaTeamRoster, error) {
	var roster LegacyArenaTeamRoster
	r := movementReader{data: body}
	var err error
	if roster.TeamID, err = r.u32(); err != nil {
		return roster, fmt.Errorf("read arena-team roster id: %w", err)
	}
	hidden, err := r.u8()
	if err != nil {
		return roster, fmt.Errorf("read arena-team roster flag: %w", err)
	}
	count, err := r.u32()
	if err != nil {
		return roster, fmt.Errorf("read arena-team roster count: %w", err)
	}
	if roster.TeamSize, err = r.u32(); err != nil {
		return roster, fmt.Errorf("read arena-team roster size: %w", err)
	}
	if count > 64 {
		return roster, fmt.Errorf("arena-team roster has %d members", count)
	}
	roster.Members = make([]LegacyArenaMember, 0, count)
	for index := uint32(0); index < count; index++ {
		member, readErr := readLegacyArenaMember(&r, hidden != 0)
		if readErr != nil {
			return roster, readErr
		}
		roster.Members = append(roster.Members, member)
	}
	if err = r.done(); err != nil {
		return roster, fmt.Errorf("arena-team roster: %w", err)
	}
	return roster, nil
}

func readLegacyArenaMember(r *movementReader, hidden bool) (LegacyArenaMember, error) {
	var member LegacyArenaMember
	var err error
	if member.GUID, err = r.u64(); err != nil {
		return member, fmt.Errorf("read arena member guid: %w", err)
	}
	online, err := r.u8()
	if err != nil {
		return member, fmt.Errorf("read arena member online: %w", err)
	}
	member.Online = online != 0
	if member.Name, err = r.cstring(); err != nil {
		return member, fmt.Errorf("read arena member name: %w", err)
	}
	captain, err := r.u32()
	if err != nil {
		return member, fmt.Errorf("read arena member captain: %w", err)
	}
	member.Captain = int32(captain)
	if member.Level, err = r.u8(); err != nil {
		return member, fmt.Errorf("read arena member level: %w", err)
	}
	if member.Class, err = r.u8(); err != nil {
		return member, fmt.Errorf("read arena member class: %w", err)
	}
	stats := []*uint32{
		&member.WeekGames, &member.WeekWins, &member.SeasonGames,
		&member.SeasonWins, &member.PersonalRating,
	}
	for _, stat := range stats {
		if *stat, err = r.u32(); err != nil {
			return member, fmt.Errorf("read arena member stats: %w", err)
		}
	}
	if hidden {
		var floats [2]float32
		for index := range floats {
			if floats[index], err = r.f32(); err != nil {
				return member, fmt.Errorf("read arena member hidden rating: %w", err)
			}
		}
		member.HiddenRating = &floats
	}
	return member, nil
}

func ParseLegacyArenaTeamCommand(body []byte) (ArenaTeamCommand, error) {
	var command ArenaTeamCommand
	r := movementReader{data: body}
	var err error
	if command.Action, err = r.u32(); err != nil {
		return command, fmt.Errorf("read arena command action: %w", err)
	}
	if command.TeamName, err = r.cstring(); err != nil {
		return command, fmt.Errorf("read arena command team: %w", err)
	}
	if command.PlayerName, err = r.cstring(); err != nil {
		return command, fmt.Errorf("read arena command player: %w", err)
	}
	if command.Error, err = r.u32(); err != nil {
		return command, fmt.Errorf("read arena command error: %w", err)
	}
	if err = r.done(); err != nil {
		return command, fmt.Errorf("arena command: %w", err)
	}
	return command, nil
}

func ParseLegacyArenaTeamEvent(body []byte) (ArenaTeamNotice, error) {
	var notice ArenaTeamNotice
	r := movementReader{data: body}
	event, err := r.u8()
	if err != nil {
		return notice, fmt.Errorf("read arena event: %w", err)
	}
	notice.Event = event + 1
	count, err := r.u8()
	if err != nil {
		return notice, fmt.Errorf("read arena event strings: %w", err)
	}
	if count > 3 {
		return notice, fmt.Errorf("arena event has %d strings", count)
	}
	for index := uint8(0); index < count; index++ {
		if notice.Params[index], err = r.cstring(); err != nil {
			return notice, fmt.Errorf("read arena event string: %w", err)
		}
	}
	if r.remaining() == 0 {
		return notice, nil
	}
	if _, err = r.u64(); err != nil {
		return notice, fmt.Errorf("read arena event guid: %w", err)
	}
	if err = r.done(); err != nil {
		return notice, fmt.Errorf("arena event: %w", err)
	}
	return notice, nil
}

func ParseLegacyArenaTeamInvite(body []byte) (ArenaTeamInviteNames, error) {
	var invite ArenaTeamInviteNames
	r := movementReader{data: body}
	var err error
	if invite.PlayerName, err = r.cstring(); err != nil {
		return invite, fmt.Errorf("read arena invite player: %w", err)
	}
	if invite.TeamName, err = r.cstring(); err != nil {
		return invite, fmt.Errorf("read arena invite team: %w", err)
	}
	if err = r.done(); err != nil {
		return invite, fmt.Errorf("arena invite: %w", err)
	}
	return invite, nil
}

func EncodeArenaTeamRoster(roster LegacyArenaTeamRoster, stats ArenaTeamStats, guidOf func(uint64) GUID128) ([]byte, error) {
	if guidOf == nil {
		guidOf = func(guid uint64) GUID128 { return ModernGUIDForLegacy(guid, 0) }
	}
	body := binary.LittleEndian.AppendUint32(nil, roster.TeamID)
	body = binary.LittleEndian.AppendUint32(body, roster.TeamSize)
	body = binary.LittleEndian.AppendUint32(body, stats.WeekPlayed)
	body = binary.LittleEndian.AppendUint32(body, stats.WeekWins)
	body = binary.LittleEndian.AppendUint32(body, stats.SeasonPlayed)
	body = binary.LittleEndian.AppendUint32(body, stats.SeasonWins)
	body = binary.LittleEndian.AppendUint32(body, stats.Rating)
	body = binary.LittleEndian.AppendUint32(body, stats.Rank)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(roster.Members)))
	bits := newBitWriter(body)
	bits.writeBit(false) // UnkBit
	body = bits.flush()
	for _, member := range roster.Members {
		encoded, err := encodeArenaMember(member, guidOf(member.GUID))
		if err != nil {
			return nil, err
		}
		body = append(body, encoded...)
	}
	return body, nil
}

func encodeArenaMember(member LegacyArenaMember, guid GUID128) ([]byte, error) {
	if len(member.Name) >= 1<<6 {
		return nil, fmt.Errorf("arena member name is %d bytes", len(member.Name))
	}
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	if member.Online {
		body = append(body, 1)
	} else {
		body = append(body, 0)
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(member.Captain))
	body = append(body, member.Level, member.Class)
	body = binary.LittleEndian.AppendUint32(body, member.WeekGames)
	body = binary.LittleEndian.AppendUint32(body, member.WeekWins)
	body = binary.LittleEndian.AppendUint32(body, member.SeasonGames)
	body = binary.LittleEndian.AppendUint32(body, member.SeasonWins)
	body = binary.LittleEndian.AppendUint32(body, member.PersonalRating)
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(member.Name)), 6)
	bits.writeBit(member.HiddenRating != nil)
	bits.writeBit(member.HiddenRating != nil)
	body = bits.flush()
	body = append(body, member.Name...)
	if member.HiddenRating != nil {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(member.HiddenRating[0]))
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(member.HiddenRating[1]))
	}
	return body, nil
}

func EncodeArenaTeamQueryResponse(emblem ArenaTeamEmblem) ([]byte, error) {
	if len(emblem.Name) >= 1<<7 {
		return nil, fmt.Errorf("arena team name is %d bytes", len(emblem.Name))
	}
	body := binary.LittleEndian.AppendUint32(nil, emblem.TeamID)
	bits := newBitWriter(body)
	bits.writeBit(true)
	body = bits.flush()
	body = binary.LittleEndian.AppendUint32(body, emblem.TeamID)
	body = binary.LittleEndian.AppendUint32(body, emblem.TeamSize)
	body = binary.LittleEndian.AppendUint32(body, emblem.BackgroundColor)
	body = binary.LittleEndian.AppendUint32(body, emblem.EmblemStyle)
	body = binary.LittleEndian.AppendUint32(body, emblem.EmblemColor)
	body = binary.LittleEndian.AppendUint32(body, emblem.BorderStyle)
	body = binary.LittleEndian.AppendUint32(body, emblem.BorderColor)
	bits = newBitWriter(body)
	bits.writeBits(uint32(len(emblem.Name)), 7)
	body = bits.flush()
	return append(body, emblem.Name...), nil
}

func EncodeArenaTeamCommand(command ArenaTeamCommand) ([]byte, error) {
	if command.Action > 255 || command.Error > 255 {
		return nil, fmt.Errorf("arena command action %d error %d does not fit a byte", command.Action, command.Error)
	}
	body, err := appendBitStrings([]byte{byte(command.Action), byte(command.Error)}, []int{7, 6}, []string{command.TeamName, command.PlayerName})
	if err != nil {
		return nil, fmt.Errorf("arena command: %w", err)
	}
	return body, nil
}

func EncodeArenaTeamEvent(notice ArenaTeamNotice) ([]byte, error) {
	body, err := appendBitStrings([]byte{notice.Event}, []int{9, 9, 9}, notice.Params[:])
	if err != nil {
		return nil, fmt.Errorf("arena event: %w", err)
	}
	return body, nil
}

func EncodeArenaTeamInvite(player, team GUID128, realmAddress uint32, names ArenaTeamInviteNames) ([]byte, error) {
	body := appendPackedGUID128(nil, player.Low, player.High)
	body = binary.LittleEndian.AppendUint32(body, realmAddress)
	body = appendPackedGUID128(body, team.Low, team.High)
	body, err := appendBitStrings(body, []int{6, 7}, []string{names.PlayerName, names.TeamName})
	if err != nil {
		return nil, fmt.Errorf("arena invite: %w", err)
	}
	return body, nil
}

func ParseArenaTeamAccept(body []byte) error {
	_, _, consumed, err := readPackedGUID128(body)
	if err != nil {
		return fmt.Errorf("arena-team accept player: %w", err)
	}
	_, _, next, err := readPackedGUID128(body[consumed:])
	if err != nil {
		return fmt.Errorf("arena-team accept team: %w", err)
	}
	if consumed+next != len(body) {
		return fmt.Errorf("arena-team accept has %d trailing bytes", len(body)-consumed-next)
	}
	return nil
}

func ParseArenaTeamDisband(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("arena-team disband has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func ParseArenaTeamRemove(body []byte) (uint32, GUID128, error) {
	if len(body) < 4 {
		return 0, GUID128{}, fmt.Errorf("arena-team remove has %d bytes", len(body))
	}
	low, high, consumed, err := readPackedGUID128(body[4:])
	if err != nil {
		return 0, GUID128{}, fmt.Errorf("arena-team remove player: %w", err)
	}
	if consumed+4 != len(body) {
		return 0, GUID128{}, fmt.Errorf("arena-team remove has %d trailing bytes", len(body)-consumed-4)
	}
	return binary.LittleEndian.Uint32(body), GUID128{Low: low, High: high}, nil
}

func ParseBattlemasterJoinArena(body []byte) (BattlemasterJoin, error) {
	var join BattlemasterJoin
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		return join, fmt.Errorf("arena join battlemaster: %w", err)
	}
	rest := body[consumed:]
	if len(rest) != 2 {
		return join, fmt.Errorf("arena join has %d bytes after the guid, want 2", len(rest))
	}
	if rest[0] > 2 {
		return join, fmt.Errorf("arena join slot %d is invalid", rest[0])
	}
	join.GUID = GUID128{Low: low, High: high}
	join.Slot = rest[0]
	join.AsGroup = true
	return join, nil
}

func ParseBattlemasterJoinSkirmish(body []byte) (BattlemasterJoin, error) {
	var join BattlemasterJoin
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		return join, fmt.Errorf("skirmish battlemaster: %w", err)
	}
	r := movementReader{data: body[consumed:]}
	if _, err = r.u8(); err != nil {
		return join, fmt.Errorf("read skirmish roles: %w", err)
	}
	slot, err := r.u8()
	if err != nil {
		return join, fmt.Errorf("read skirmish bracket: %w", err)
	}
	normalized, ok := arenaQueueSlot(slot)
	if !ok {
		return join, fmt.Errorf("skirmish bracket %d is invalid", slot)
	}
	asGroup, err := r.bit()
	if err != nil {
		return join, fmt.Errorf("read skirmish group flag: %w", err)
	}
	if _, err = r.bit(); err != nil {
		return join, fmt.Errorf("read skirmish requeue flag: %w", err)
	}
	if err = r.done(); err != nil {
		return join, fmt.Errorf("skirmish: %w", err)
	}
	join.GUID = GUID128{Low: low, High: high}
	join.Slot = normalized
	join.AsGroup = asGroup
	return join, nil
}

// arenaQueueSlot accepts a legacy slot (0/1/2) or an unambiguous team size.
// Size 2 is also slot 2 (5v5), so it stays 2.
func arenaQueueSlot(value byte) (byte, bool) {
	switch value {
	case 0, 1, 2:
		return value, true
	case 3:
		return 1, true
	case 5:
		return 2, true
	default:
		return 0, false
	}
}

func EncodeLegacyArenaTeamID(teamID uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, teamID)
}

func EncodeLegacyArenaTeamRemove(teamID uint32, name string) []byte {
	return appendCString(EncodeLegacyArenaTeamID(teamID), name)
}

func EncodeLegacyBattlemasterJoin(guid uint64, slot, asGroup, rated byte) []byte {
	body := binary.LittleEndian.AppendUint64(nil, guid)
	return append(body, slot, asGroup, rated)
}

func appendBitStrings(body []byte, widths []int, values []string) ([]byte, error) {
	if len(widths) != len(values) {
		return nil, fmt.Errorf("bit string count mismatch")
	}
	bits := newBitWriter(body)
	for index, value := range values {
		if len(value) >= 1<<widths[index] {
			return nil, fmt.Errorf("string %d is %d bytes", index, len(value))
		}
		bits.writeBits(uint32(len(value)), widths[index])
	}
	body = bits.flush()
	for _, value := range values {
		body = append(body, value...)
	}
	return body, nil
}

func (r *movementReader) done() error {
	if r.bitOffset == 0 {
		if r.offset != len(r.data) {
			return fmt.Errorf("%d trailing bytes", len(r.data)-r.offset)
		}
		return nil
	}
	if r.offset != len(r.data)-1 {
		return fmt.Errorf("%d trailing bytes", len(r.data)-r.offset-1)
	}
	return nil
}

func ParseBattlePetRequestJournalLock(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("battle-pet journal-lock request has %d bytes, want 0", len(body))
	}
	return nil
}
