package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Character pane extras: titles, honor/PvP inspect and the character-rename
// flow. Mirrors HermesProxy-WOTLK World/Server/Packets
// {TitleEarned,InspectHonorStatsResultTBC,InspectPvP,ArenaTeamInspectData,
// CharacterRenameRequest,CharacterRenameResult}.cs and the matching
// World/Client/WorldClient.cs + World/Server/WorldSocket.cs handlers.
// Rune synchronization is translated separately in rune.go. SET_REST_START
// has no 3.4.3 twin; rest state rides the active-player update fields.

const (
	// Client -> server character-pane controls (build 54261).
	CMSGInspectPvp             = uint16(13987) // 0x36A3
	CMSGCharacterRenameRequest = uint16(14025) // 0x36C9

	// Server -> client character-pane data.
	SMSGTitleEarned           = uint16(9943)
	SMSGInspectPvp            = uint16(10018)
	SMSGInspectHonorStats     = uint16(10547)
	SMSGCharacterRenameResult = uint16(10087)
)

// ArenaSlotCount is the number of arena team slots a player can belong to.
const ArenaSlotCount = 3

const arenaInspectTeamType = uint64(52) << 58 // HighGuidType703.ArenaTeam

// TitleEarned ---------------------------------------------------------------

// ParseLegacyTitleEarned reads the legacy SMSG_TITLE_EARNED payload: the title
// index and the earned flag (0 = lost). 3.4.3 has no title-lost packet, so a
// lost title is dropped by the relay.
func ParseLegacyTitleEarned(body []byte) (index uint32, earned bool, err error) {
	r := movementReader{data: body}
	if index, err = r.u32(); err != nil {
		return 0, false, fmt.Errorf("read title index: %w", err)
	}
	earnedValue, readErr := r.u32()
	if readErr != nil {
		return 0, false, fmt.Errorf("read title earned flag: %w", readErr)
	}
	if r.remaining() != 0 {
		return 0, false, fmt.Errorf("title earned has %d trailing bytes", r.remaining())
	}
	return index, earnedValue != 0, nil
}

func EncodeTitleEarned(index uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, index)
}

// Honor inspect -------------------------------------------------------------

// LegacyHonorInspect mirrors the 3.3.5a MSG_INSPECT_HONOR_STATS payload.
type LegacyHonorInspect struct {
	GUID                uint64
	LifetimeHighestRank uint8
	TodayHK             uint16
	YesterdayHK         uint16
	TodayHonor          uint32
	YesterdayHonor      uint32
	LifetimeHK          uint32
}

func ParseLegacyHonorInspect(body []byte) (LegacyHonorInspect, error) {
	var honor LegacyHonorInspect
	r := movementReader{data: body}
	var err error
	if honor.GUID, err = r.u64(); err != nil {
		return honor, fmt.Errorf("read honor-inspect player: %w", err)
	}
	if honor.LifetimeHighestRank, err = r.u8(); err != nil {
		return honor, fmt.Errorf("read honor-inspect rank: %w", err)
	}
	if honor.TodayHK, err = r.u16(); err != nil {
		return honor, fmt.Errorf("read honor-inspect today kills: %w", err)
	}
	if honor.YesterdayHK, err = r.u16(); err != nil {
		return honor, fmt.Errorf("read honor-inspect yesterday kills: %w", err)
	}
	if honor.TodayHonor, err = r.u32(); err != nil {
		return honor, fmt.Errorf("read honor-inspect today honor: %w", err)
	}
	if honor.YesterdayHonor, err = r.u32(); err != nil {
		return honor, fmt.Errorf("read honor-inspect yesterday honor: %w", err)
	}
	if honor.LifetimeHK, err = r.u32(); err != nil {
		return honor, fmt.Errorf("read honor-inspect lifetime kills: %w", err)
	}
	if r.remaining() != 0 {
		return honor, fmt.Errorf("honor-inspect has %d trailing bytes", r.remaining())
	}
	return honor, nil
}

// EncodeHonorInspectResult writes the build-54261 SMSG_INSPECT_HONOR_STATS in
// the TBC layout Hermes uses for the Wrath client: only the highest rank, the
// yesterday and lifetime honorable-kill counters carry real values; the rest are
// protocol padding.
func EncodeHonorInspectResult(honor LegacyHonorInspect, resolve func(uint64) GUID128) []byte {
	player := resolve(honor.GUID)
	body := appendPackedGUID128(nil, player.Low, player.High)
	body = append(body, honor.LifetimeHighestRank)
	body = binary.LittleEndian.AppendUint16(body, 0) // Unused1
	body = binary.LittleEndian.AppendUint16(body, honor.YesterdayHK)
	body = binary.LittleEndian.AppendUint16(body, 0) // Unused3
	body = binary.LittleEndian.AppendUint16(body, uint16(honor.LifetimeHK))
	body = binary.LittleEndian.AppendUint32(body, 0) // Unused4..8
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	return append(body, 0) // Unused9
}

// Arena / PvP inspect -------------------------------------------------------

// ArenaTeamInspect mirrors one arena team record in MSG_INSPECT_ARENA_TEAMS.
type ArenaTeamInspect struct {
	Slot           uint8
	TeamID         uint32
	Rating         int32
	GamesPlayed    int32
	GamesWon       int32
	PersonalGames  int32
	PersonalRating int32
}

// ParseLegacyArenaTeamInspect reads one legacy MSG_INSPECT_ARENA_TEAMS record:
// the inspected player GUID, the arena slot (0..2), the team id and the team /
// personal rating history.
func ParseLegacyArenaTeamInspect(body []byte) (guid uint64, team ArenaTeamInspect, err error) {
	r := movementReader{data: body}
	if guid, err = r.u64(); err != nil {
		return 0, team, fmt.Errorf("read arena-inspect player: %w", err)
	}
	if team.Slot, err = r.u8(); err != nil {
		return 0, team, fmt.Errorf("read arena-inspect slot: %w", err)
	}
	if team.Slot >= ArenaSlotCount {
		return 0, team, fmt.Errorf("arena-inspect slot %d out of range", team.Slot)
	}
	if team.TeamID, err = r.u32(); err != nil {
		return 0, team, fmt.Errorf("read arena-inspect team id: %w", err)
	}
	if team.Rating, err = r.i32(); err != nil {
		return 0, team, fmt.Errorf("read arena-inspect rating: %w", err)
	}
	if team.GamesPlayed, err = r.i32(); err != nil {
		return 0, team, fmt.Errorf("read arena-inspect games played: %w", err)
	}
	if team.GamesWon, err = r.i32(); err != nil {
		return 0, team, fmt.Errorf("read arena-inspect games won: %w", err)
	}
	if team.PersonalGames, err = r.i32(); err != nil {
		return 0, team, fmt.Errorf("read arena-inspect personal games: %w", err)
	}
	if team.PersonalRating, err = r.i32(); err != nil {
		return 0, team, fmt.Errorf("read arena-inspect personal rating: %w", err)
	}
	if r.remaining() != 0 {
		return 0, team, fmt.Errorf("arena-inspect has %d trailing bytes", r.remaining())
	}
	return guid, team, nil
}

func ModernArenaTeamGUID(teamID uint32) GUID128 {
	if teamID == 0 {
		return GUID128{}
	}
	return GUID128{Low: uint64(teamID), High: arenaInspectTeamType}
}

// EncodeInspectPvp writes SMSG_INSPECT_PVP with the three arena slots. A slot
// the inspected player is not part of carries an empty team guid and zeroed
// rating history.
func EncodeInspectPvp(player GUID128, teams [ArenaSlotCount]ArenaTeamInspect) []byte {
	body := appendPackedGUID128(nil, player.Low, player.High)
	bits := newBitWriter(body)
	bits.writeBits(0, 3)              // Brackets.Count
	bits.writeBits(ArenaSlotCount, 2) // ArenaTeams.Count
	body = bits.flush()
	for _, team := range teams {
		guid := ModernArenaTeamGUID(team.TeamID)
		body = appendPackedGUID128(body, guid.Low, guid.High)
		body = binary.LittleEndian.AppendUint32(body, uint32(team.Rating))
		body = binary.LittleEndian.AppendUint32(body, uint32(team.GamesPlayed))
		body = binary.LittleEndian.AppendUint32(body, uint32(team.GamesWon))
		body = binary.LittleEndian.AppendUint32(body, uint32(team.PersonalGames))
		body = binary.LittleEndian.AppendUint32(body, uint32(team.PersonalRating))
	}
	return body
}

// Character rename ----------------------------------------------------------

// CharacterRenameRequest is the modern CMSG_CHARACTER_RENAME_REQUEST payload.
type CharacterRenameRequest struct {
	GUID GUID128
	Name string
}

func ParseCharacterRenameRequest(body []byte) (CharacterRenameRequest, error) {
	var request CharacterRenameRequest
	r := movementReader{data: body}
	var err error
	if request.GUID, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read rename character: %w", err)
	}
	nameLength, readErr := r.bits(6)
	if readErr != nil {
		return request, fmt.Errorf("read rename name length: %w", readErr)
	}
	if request.Name, err = r.stringN(int(nameLength)); err != nil {
		return request, fmt.Errorf("read rename name: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("rename request has %d trailing bytes", r.remaining())
	}
	return request, nil
}

// EncodeLegacyCharacterRename writes the WotLK CMSG_CHARACTER_RENAME_REQUEST:
// a raw character GUID64 plus the NUL-terminated new name.
func EncodeLegacyCharacterRename(legacyGUID uint64, name string) []byte {
	body := binary.LittleEndian.AppendUint64(nil, legacyGUID)
	return appendCString(body, name)
}

// LegacyCharacterRenameResult mirrors the legacy SMSG_CHARACTER_RENAME_RESULT.
type LegacyCharacterRenameResult struct {
	Result byte
	GUID   uint64
	Name   string
}

func ParseLegacyCharacterRenameResult(body []byte) (LegacyCharacterRenameResult, error) {
	var result LegacyCharacterRenameResult
	r := movementReader{data: body}
	var err error
	if result.Result, err = r.u8(); err != nil {
		return result, fmt.Errorf("read rename result: %w", err)
	}
	if result.Result == 0 {
		if result.GUID, err = r.u64(); err != nil {
			return result, fmt.Errorf("read rename result character: %w", err)
		}
		if result.Name, err = r.cstring(); err != nil {
			return result, fmt.Errorf("read rename result name: %w", err)
		}
	}
	if r.remaining() != 0 {
		return result, fmt.Errorf("rename result has %d trailing bytes", r.remaining())
	}
	return result, nil
}

// EncodeCharacterRenameResult writes the build-54261 SMSG_CHARACTER_RENAME_RESULT:
// a one-byte result, a has-guid bit and a 6-bit name length, then the packed
// GUID128 and name on success.
func EncodeCharacterRenameResult(result LegacyCharacterRenameResult, resolve func(uint64) GUID128) []byte {
	body := []byte{result.Result}
	hasGUID := result.Result == 0 && result.GUID != 0
	bits := newBitWriter(body)
	bits.writeBit(hasGUID)
	bits.writeBits(uint32(len(result.Name)), 6)
	body = bits.flush()
	if hasGUID {
		guid := resolve(result.GUID)
		body = appendPackedGUID128(body, guid.Low, guid.High)
	}
	return append(body, result.Name...)
}
