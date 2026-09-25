package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Battleground packets between AzerothCore 3.3.5a and build 54261.
// Legacy fields follow BattlegroundMgr::BuildBattlegroundStatusPacket and
// Battleground::BuildPvPLogDataPacket. 54261 writes follow legacy proxy's expansion-80
// serializers. Leaving a queue is SMSG_BATTLEFIELD_STATUS_NONE, not Hermes's
// failed reason 30. Port and leave echo the arena-type byte from the status
// header; Hermes writes 2 for every battleground. The scoreboard opcode is
// legacy proxy Write343's 0x2934. Sex is one byte on this build.

const (
	CMSGBattlefieldLeave        = uint16(0x3175)
	CMSGPvpLogData              = uint16(0x317F)
	CMSGBattlefieldList         = uint16(0x3181)
	CMSGAreaSpiritHealerQuery   = uint16(0x34B0)
	CMSGAreaSpiritHealerQueue   = uint16(0x34B1)
	CMSGBattlemasterJoin        = uint16(0x3520)
	CMSGBattlefieldPort         = uint16(0x3525)
	SMSGAreaSpiritHealerTime    = uint16(0x2740)
	SMSGBattlefieldStatusNeed   = uint16(0x2922)
	SMSGBattlefieldStatusActive = uint16(0x2923)
	SMSGBattlefieldStatusQueued = uint16(0x2924)
	SMSGBattlefieldStatusNone   = uint16(0x2925)
	SMSGBattlefieldStatusFailed = uint16(0x2926)
	SMSGBattlefieldList         = uint16(0x2927)
	SMSGBattlegroundPositions   = uint16(0x2928)
	SMSGBattlegroundPlayerJoin  = uint16(0x292B)
	SMSGBattlegroundPlayerLeft  = uint16(0x292C)
	SMSGPvpLogData              = uint16(0x2934)
	SMSGPvpCredit               = uint16(0x294A)
	SMSGBattlegroundInit        = uint16(0x294F)
	SMSGPlayerSkinned           = uint16(0x3006)

	battlegroundRideType         = uint32(1)
	battlegroundQueueMask        = uint64(0x1F10000000000000)
	battlegroundPortConstant     = uint16(0x1F90)
	battlegroundListVerification = int32(121761856)
	battlegroundInitMilliseconds = uint32(1154756799)
	battlegroundListLevelMin     = byte(1)
	battlegroundListLevelMax     = byte(80)
	battlegroundHonorLevel       = int32(1)
	battlegroundStatusNone       = uint32(0)
	battlegroundStatusQueued     = uint32(1)
	battlegroundStatusConfirm    = uint32(2)
	battlegroundStatusActive     = uint32(3)
	battlegroundJoinTimedOut     = int32(-11)
	battlegroundJoinFailed       = int32(-12)
	battlegroundMaxPlayers       = 80
	battlegroundMaxObjectives    = 16
	battlegroundMaxInstances     = 64
	battlegroundMaxPositions     = 128
	battlegroundArenaMarker      = byte(0x0E)
)

// BattlegroundQueue is the 8-byte prefix AzerothCore expects on port and leave.
// ArenaType is 0 for a battleground and 2/3/5 for an arena. Marker is 0x0E
// when the legacy battleground is an arena.
type BattlegroundQueue struct {
	ArenaType byte
	Marker    byte
	BgTypeID  uint32
	JoinedAt  int64
}

// BattlegroundPlayer is race, class, and sex for one scoreboard row.
type BattlegroundPlayer struct {
	Race  byte
	Class byte
	Sex   byte
	Known bool
}

// BattlegroundContext carries the session values the status and scoreboard
// translations need. Queues is updated in place and must be non-nil when the
// caller wants the port header remembered.
type BattlegroundContext struct {
	Requester GUID128
	Now       int64
	Queues    map[uint32]BattlegroundQueue
	GUID      func(uint64) GUID128
	Player    func(uint64) BattlegroundPlayer
}

// BattlegroundJoin is CMSG_BATTLEMASTER_JOIN after the roles, blacklist, and
// verification fields are dropped.
type BattlegroundJoin struct {
	BgTypeID     uint32
	Battlemaster GUID128
	InstanceID   uint32
	AsGroup      bool
}

// BattlefieldPort is CMSG_BATTLEFIELD_PORT reduced to the ticket the legacy
// header was stored under, plus the accept bit.
type BattlefieldPort struct {
	TicketID uint32
	Accept   bool
}

// BattlegroundPlayerFromFields prefers the live unit bytes, then the character
// list. A zero race is treated as unknown.
func BattlegroundPlayerFromFields(fields map[int]uint32, character LegacyCharacter, hasCharacter bool) BattlegroundPlayer {
	if fields != nil {
		raw, ok := fields[legacyUnitBytes0]
		if ok && byte(raw) != 0 {
			return BattlegroundPlayer{Race: byte(raw), Class: byte(raw >> 8), Sex: byte(raw >> 16), Known: true}
		}
	}
	if hasCharacter && character.Race != 0 {
		return BattlegroundPlayer{Race: character.Race, Class: character.Class, Sex: character.Sex, Known: true}
	}
	return BattlegroundPlayer{}
}

// TranslateLegacyBattlefieldStatus splits one WotLK status packet into the
// 54261 queued, confirmation, active, or none packet. An active status whose
// shutdown timer is 0 is preceded by SMSG_BATTLEGROUND_INIT.
func TranslateLegacyBattlefieldStatus(body []byte, ctx BattlegroundContext) ([]Packet, error) {
	parsed, err := parseLegacyBattlefieldStatus(body)
	if err != nil {
		return nil, err
	}
	ticketID := 1 + parsed.Slot
	if parsed.None {
		when := ctx.Now
		if existing, ok := ctx.Queues[ticketID]; ok && existing.JoinedAt != 0 {
			when = existing.JoinedAt
		}
		if ctx.Queues != nil {
			delete(ctx.Queues, ticketID)
		}
		return []Packet{{
			Opcode: SMSGBattlefieldStatusNone,
			Body:   appendBattlegroundTicket(nil, ctx.Requester, ticketID, when),
		}}, nil
	}
	when := ctx.Now
	if existing, ok := ctx.Queues[ticketID]; ok && existing.JoinedAt != 0 {
		when = existing.JoinedAt
	}
	if ctx.Queues != nil {
		ctx.Queues[ticketID] = BattlegroundQueue{
			ArenaType: parsed.ArenaType,
			Marker:    parsed.Marker,
			BgTypeID:  parsed.BgTypeID,
			JoinedAt:  when,
		}
	}
	header := appendBattlegroundHeader(nil, ctx.Requester, ticketID, when, parsed)
	switch parsed.Status {
	case battlegroundStatusQueued:
		body := header
		body = binary.LittleEndian.AppendUint32(body, parsed.Time1)
		body = binary.LittleEndian.AppendUint32(body, parsed.Time2)
		body = appendBits(body, false, true, false)
		return []Packet{{Opcode: SMSGBattlefieldStatusQueued, Body: body}}, nil
	case battlegroundStatusConfirm:
		body := header
		body = binary.LittleEndian.AppendUint32(body, parsed.MapID)
		body = binary.LittleEndian.AppendUint32(body, parsed.Time1)
		body = append(body, 0)
		return []Packet{{Opcode: SMSGBattlefieldStatusNeed, Body: body}}, nil
	case battlegroundStatusActive:
		var packets []Packet
		if parsed.Time1 == 0 {
			initBody := binary.LittleEndian.AppendUint32(nil, battlegroundInitMilliseconds)
			initBody = binary.LittleEndian.AppendUint16(initBody, 0)
			packets = append(packets, Packet{Opcode: SMSGBattlegroundInit, Body: initBody})
		}
		body := header
		body = binary.LittleEndian.AppendUint32(body, parsed.MapID)
		body = binary.LittleEndian.AppendUint32(body, parsed.Time1)
		body = binary.LittleEndian.AppendUint32(body, parsed.Time2)
		body = appendBits(body, parsed.ArenaFaction != 0, false)
		packets = append(packets, Packet{Opcode: SMSGBattlefieldStatusActive, Body: body})
		return packets, nil
	default:
		return nil, fmt.Errorf("battlefield status %d is unknown", parsed.Status)
	}
}

// TranslateLegacyBattlefieldList rewrites SMSG_BATTLEFIELD_LIST. AzerothCore
// writes the two level bytes as 0; 54261 uses them as a level filter, so 0,0
// becomes 1,80.
func TranslateLegacyBattlefieldList(body []byte, guid func(uint64) GUID128) (Packet, error) {
	parsed, err := parseLegacyBattlefieldList(body)
	if err != nil {
		return Packet{}, err
	}
	minLevel, maxLevel := parsed.MinLevel, parsed.MaxLevel
	if minLevel == 0 && maxLevel == 0 {
		minLevel = battlegroundListLevelMin
		maxLevel = battlegroundListLevelMax
	}
	encoded := appendPackedGUID128(nil, guid(parsed.Battlemaster).Low, guid(parsed.Battlemaster).High)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(battlegroundListVerification))
	encoded = binary.LittleEndian.AppendUint32(encoded, parsed.BgTypeID)
	encoded = append(encoded, minLevel, maxLevel)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(parsed.Instances)))
	for _, instance := range parsed.Instances {
		encoded = binary.LittleEndian.AppendUint32(encoded, instance)
	}
	encoded = appendBits(encoded, parsed.FromUI, parsed.RandomWinToday)
	return Packet{Opcode: SMSGBattlefieldList, Body: encoded}, nil
}

// TranslateLegacyPVPLog rewrites MSG_PVP_LOG_DATA as SMSG_PVP_LOG_DATA 0x2934.
// Arena rating triples are not prematch and postmatch ratings, so RatingData
// stays absent.
func TranslateLegacyPVPLog(body []byte, ctx BattlegroundContext) (Packet, error) {
	parsed, err := parseLegacyPVPLog(body)
	if err != nil {
		return Packet{}, err
	}
	encoded := appendPVPLogHeader(parsed.Arena, parsed.Winner != nil, parsed.Names)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(parsed.Players)))
	encoded = append(encoded, byte(parsed.Counts[0]), byte(parsed.Counts[1]))
	if parsed.Winner != nil {
		encoded = append(encoded, *parsed.Winner)
	}
	for _, player := range parsed.Players {
		encoded = appendPVPPlayer(encoded, player, ctx)
	}
	return Packet{Opcode: SMSGPvpLogData, Body: encoded}, nil
}

// TranslateLegacyBattlegroundPositions forwards flag carriers and drops the
// ordinary member positions AzerothCore currently leaves at count 0.
func TranslateLegacyBattlegroundPositions(body []byte, ctx BattlegroundContext) (Packet, error) {
	carriers, err := parseLegacyBattlegroundPositions(body)
	if err != nil {
		return Packet{}, err
	}
	encoded := binary.LittleEndian.AppendUint32(nil, uint32(len(carriers)))
	for _, carrier := range carriers {
		player := ctx.player(carrier.GUID)
		icon, slot := battlegroundFlagIcon(player)
		modern := ctx.guid(carrier.GUID)
		encoded = appendPackedGUID128(encoded, modern.Low, modern.High)
		encoded = appendFloat32(encoded, carrier.X)
		encoded = appendFloat32(encoded, carrier.Y)
		encoded = append(encoded, byte(icon), byte(slot))
	}
	return Packet{Opcode: SMSGBattlegroundPositions, Body: encoded}, nil
}

// TranslateLegacyBattlegroundPlayer rewrites a joined or left player GUID.
func TranslateLegacyBattlegroundPlayer(body []byte, opcode uint16, guid func(uint64) GUID128) (Packet, error) {
	legacy, err := parseLegacyGUIDPacket(body, "battleground player")
	if err != nil {
		return Packet{}, err
	}
	modern := guid(legacy)
	return Packet{Opcode: opcode, Body: appendPackedGUID128(nil, modern.Low, modern.High)}, nil
}

// TranslateLegacyAreaSpiritHealerTime rewrites the healer GUID and remaining time.
func TranslateLegacyAreaSpiritHealerTime(body []byte, guid func(uint64) GUID128) (Packet, error) {
	r := movementReader{data: body}
	legacy, err := r.u64()
	if err != nil {
		return Packet{}, fmt.Errorf("read spirit healer: %w", err)
	}
	remaining, err := r.u32()
	if err != nil {
		return Packet{}, fmt.Errorf("read spirit healer time: %w", err)
	}
	if err := finishReader(&r, "spirit healer time"); err != nil {
		return Packet{}, err
	}
	modern := guid(legacy)
	encoded := appendPackedGUID128(nil, modern.Low, modern.High)
	encoded = binary.LittleEndian.AppendUint32(encoded, remaining)
	return Packet{Opcode: SMSGAreaSpiritHealerTime, Body: encoded}, nil
}

// TranslateLegacyPVPCredit writes the honor value, a zero second honor field,
// the victim GUID, and the rank. 54261 added the extra int32.
func TranslateLegacyPVPCredit(body []byte, guid func(uint64) GUID128) (Packet, error) {
	r := movementReader{data: body}
	honor, err := r.i32()
	if err != nil {
		return Packet{}, fmt.Errorf("read pvp honor: %w", err)
	}
	target, err := r.u64()
	if err != nil {
		return Packet{}, fmt.Errorf("read pvp credit target: %w", err)
	}
	rank, err := r.u32()
	if err != nil {
		return Packet{}, fmt.Errorf("read pvp credit rank: %w", err)
	}
	if err := finishReader(&r, "pvp credit"); err != nil {
		return Packet{}, err
	}
	modern := guid(target)
	encoded := binary.LittleEndian.AppendUint32(nil, uint32(honor))
	encoded = binary.LittleEndian.AppendUint32(encoded, 0)
	encoded = appendPackedGUID128(encoded, modern.Low, modern.High)
	encoded = binary.LittleEndian.AppendUint32(encoded, rank)
	return Packet{Opcode: SMSGPvpCredit, Body: encoded}, nil
}

// TranslateLegacyPlayerSkinned forwards the optional free-repop byte as a bit.
// An empty body, which is all AzerothCore sends today, clears the bit.
func TranslateLegacyPlayerSkinned(body []byte) (Packet, error) {
	free := false
	switch len(body) {
	case 0:
	case 1:
		free = body[0] != 0
	default:
		return Packet{}, fmt.Errorf("player skinned has %d bytes, want 0 or 1", len(body))
	}
	return Packet{Opcode: SMSGPlayerSkinned, Body: appendBits(nil, free)}, nil
}

// TranslateLegacyGroupJoinedBattleground turns a negative or zero join result
// into SMSG_BATTLEFIELD_STATUS_FAILED. A positive result is the battleground
// type and is not forwarded; the status packet that follows shows the queue.
func TranslateLegacyGroupJoinedBattleground(body []byte, ctx BattlegroundContext) ([]Packet, error) {
	r := movementReader{data: body}
	result, err := r.i32()
	if err != nil {
		return nil, fmt.Errorf("read battleground join result: %w", err)
	}
	var client GUID128
	if result == battlegroundJoinTimedOut || result == battlegroundJoinFailed {
		legacy, readErr := r.u64()
		if readErr != nil {
			return nil, fmt.Errorf("read battleground join player: %w", readErr)
		}
		client = ctx.guid(legacy)
	}
	if err := finishReader(&r, "battleground join result"); err != nil {
		return nil, err
	}
	if result > 0 {
		return nil, nil
	}
	encoded := appendBattlegroundTicket(nil, ctx.Requester, 1, ctx.Now)
	encoded = binary.LittleEndian.AppendUint64(encoded, battlegroundQueueMask)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(result))
	encoded = appendPackedGUID128(encoded, client.Low, client.High)
	return []Packet{{Opcode: SMSGBattlefieldStatusFailed, Body: encoded}}, nil
}

// ParseBattlegroundJoin reads the 54261 queue request. The queue id keeps the
// battleground type in the low 32 bits under the 0x1F10000000000000 mask.
func ParseBattlegroundJoin(body []byte) (BattlegroundJoin, error) {
	r := movementReader{data: body}
	var join BattlegroundJoin
	queueID, err := r.u64()
	if err != nil {
		return join, fmt.Errorf("read battleground queue: %w", err)
	}
	join.BgTypeID = uint32(queueID &^ battlegroundQueueMask)
	if _, err = r.u8(); err != nil {
		return join, fmt.Errorf("read battleground roles: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return join, fmt.Errorf("read battleground blacklist: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return join, fmt.Errorf("read battleground blacklist: %w", err)
	}
	if join.Battlemaster, err = r.guid128(); err != nil {
		return join, fmt.Errorf("read battlemaster: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return join, fmt.Errorf("read battleground verification: %w", err)
	}
	instance, err := r.i32()
	if err != nil {
		return join, fmt.Errorf("read battleground instance: %w", err)
	}
	join.InstanceID = uint32(instance)
	if join.AsGroup, err = r.bit(); err != nil {
		return join, fmt.Errorf("read battleground group bit: %w", err)
	}
	if err := finishReader(&r, "battlemaster join"); err != nil {
		return join, err
	}
	return join, nil
}

// EncodeLegacyBattlegroundJoin writes GUID64, battleground type, instance, and
// the group byte.
func EncodeLegacyBattlegroundJoin(guid uint64, join BattlegroundJoin) []byte {
	body := binary.LittleEndian.AppendUint64(nil, guid)
	body = binary.LittleEndian.AppendUint32(body, join.BgTypeID)
	body = binary.LittleEndian.AppendUint32(body, join.InstanceID)
	group := byte(0)
	if join.AsGroup {
		group = 1
	}
	return append(body, group)
}

// ParseBattlefieldListRequest reads the 54261 list id.
func ParseBattlefieldListRequest(body []byte) (uint32, error) {
	r := movementReader{data: body}
	listID, err := r.u32()
	if err != nil {
		return 0, fmt.Errorf("read battlefield list: %w", err)
	}
	if err := finishReader(&r, "battlefield list request"); err != nil {
		return 0, err
	}
	return listID, nil
}

// EncodeLegacyBattlefieldListRequest writes the type plus fromWhere 0 and
// canGainXP 1, matching the battlemaster list request AzerothCore expects.
func EncodeLegacyBattlefieldListRequest(bgTypeID uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, bgTypeID)
	return append(body, 0, 1)
}

// ParseBattlefieldPort reads the ticket id and the accept bit.
func ParseBattlefieldPort(body []byte) (BattlefieldPort, error) {
	r := movementReader{data: body}
	var port BattlefieldPort
	if _, err := r.guid128(); err != nil {
		return port, fmt.Errorf("read battlefield port ticket: %w", err)
	}
	ticketID, err := r.u32()
	if err != nil {
		return port, fmt.Errorf("read battlefield port ticket id: %w", err)
	}
	port.TicketID = ticketID
	if _, err = r.u32(); err != nil {
		return port, fmt.Errorf("read battlefield port ticket type: %w", err)
	}
	if _, err = r.u64(); err != nil {
		return port, fmt.Errorf("read battlefield port ticket time: %w", err)
	}
	if _, err = r.bit(); err != nil {
		return port, fmt.Errorf("read battlefield port ticket bit: %w", err)
	}
	r.align()
	if port.Accept, err = r.bit(); err != nil {
		return port, fmt.Errorf("read battlefield port accept: %w", err)
	}
	if err := finishReader(&r, "battlefield port"); err != nil {
		return port, err
	}
	return port, nil
}

// EncodeLegacyBattlefieldPort writes the stored 8-byte queue header and the
// accept byte. ArenaType comes from the status packet, not a fixed 2.
func EncodeLegacyBattlefieldPort(queue BattlegroundQueue, accept bool) []byte {
	return appendLegacyQueueHeader(queue, accept)
}

// RequireBattlegroundQueue returns the header stored for a port ticket.
// An unknown ticket is an error so the proxy never invents arena type 2.
func RequireBattlegroundQueue(queues map[uint32]BattlegroundQueue, ticketID uint32) (BattlegroundQueue, error) {
	queue, ok := queues[ticketID]
	if !ok {
		return BattlegroundQueue{}, fmt.Errorf("battlefield port ticket %d is unknown", ticketID)
	}
	return queue, nil
}

// EncodeLegacyBattlefieldLeave writes the same 8-byte header. AzerothCore
// skips the fields; a missing queue still produces a parseable packet.
func EncodeLegacyBattlefieldLeave(queue *BattlegroundQueue) []byte {
	if queue == nil {
		return appendLegacyQueueHeader(BattlegroundQueue{}, false)[:8]
	}
	return appendLegacyQueueHeader(*queue, false)[:8]
}

// ParseAreaSpiritHealer reads the packed healer GUID used by both the query
// and the resurrect-queue request.
func ParseAreaSpiritHealer(body []byte) (GUID128, error) {
	guid, err := ParsePackedGUID128Exact(body)
	if err != nil {
		return GUID128{}, fmt.Errorf("read area spirit healer: %w", err)
	}
	if guid.Low == 0 && guid.High == 0 {
		return GUID128{}, fmt.Errorf("area spirit healer GUID is empty")
	}
	return guid, nil
}

// EncodeLegacyAreaSpiritHealer writes the unpacked healer GUID.
func EncodeLegacyAreaSpiritHealer(guid uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, guid)
}

// ParseEmptyBattlegroundRequest rejects a status or scoreboard request that
// carries a body. Both legacy opcodes are empty.
func ParseEmptyBattlegroundRequest(body []byte, what string) error {
	if len(body) != 0 {
		return fmt.Errorf("%s has %d bytes, want 0", what, len(body))
	}
	return nil
}

type legacyBattlefieldStatus struct {
	Slot         uint32
	None         bool
	ArenaType    byte
	Marker       byte
	BgTypeID     uint32
	MinLevel     byte
	MaxLevel     byte
	InstanceID   uint32
	Rated        bool
	Status       uint32
	Time1        uint32
	Time2        uint32
	MapID        uint32
	ArenaFaction byte
}

func parseLegacyBattlefieldStatus(body []byte) (legacyBattlefieldStatus, error) {
	r := movementReader{data: body}
	var status legacyBattlefieldStatus
	slot, err := r.u32()
	if err != nil {
		return status, fmt.Errorf("read battlefield queue slot: %w", err)
	}
	status.Slot = slot
	arenaType, err := r.u8()
	if err != nil {
		return status, fmt.Errorf("read battlefield arena type: %w", err)
	}
	marker, err := r.u8()
	if err != nil {
		return status, fmt.Errorf("read battlefield arena marker: %w", err)
	}
	bgType, err := r.u32()
	if err != nil {
		return status, fmt.Errorf("read battlefield type: %w", err)
	}
	if _, err = r.u16(); err != nil {
		return status, fmt.Errorf("read battlefield port constant: %w", err)
	}
	if arenaType == 0 && marker == 0 && bgType == 0 {
		status.None = true
		if err := finishReader(&r, "battlefield status"); err != nil {
			return status, err
		}
		return status, nil
	}
	status.ArenaType = arenaType
	status.Marker = marker
	status.BgTypeID = bgType
	if status.MinLevel, err = r.u8(); err != nil {
		return status, fmt.Errorf("read battlefield min level: %w", err)
	}
	if status.MaxLevel, err = r.u8(); err != nil {
		return status, fmt.Errorf("read battlefield max level: %w", err)
	}
	if status.InstanceID, err = r.u32(); err != nil {
		return status, fmt.Errorf("read battlefield instance: %w", err)
	}
	rated, err := r.u8()
	if err != nil {
		return status, fmt.Errorf("read battlefield rated flag: %w", err)
	}
	status.Rated = rated != 0
	if status.Status, err = r.u32(); err != nil {
		return status, fmt.Errorf("read battlefield status id: %w", err)
	}
	switch status.Status {
	case battlegroundStatusQueued:
		if status.Time1, err = r.u32(); err != nil {
			return status, fmt.Errorf("read battlefield average wait: %w", err)
		}
		if status.Time2, err = r.u32(); err != nil {
			return status, fmt.Errorf("read battlefield wait: %w", err)
		}
	case battlegroundStatusConfirm:
		if status.MapID, err = r.u32(); err != nil {
			return status, fmt.Errorf("read battlefield map: %w", err)
		}
		if _, err = r.u64(); err != nil {
			return status, fmt.Errorf("read battlefield confirm padding: %w", err)
		}
		if status.Time1, err = r.u32(); err != nil {
			return status, fmt.Errorf("read battlefield confirm timeout: %w", err)
		}
	case battlegroundStatusActive:
		if status.MapID, err = r.u32(); err != nil {
			return status, fmt.Errorf("read battlefield map: %w", err)
		}
		if _, err = r.u64(); err != nil {
			return status, fmt.Errorf("read battlefield active padding: %w", err)
		}
		if status.Time1, err = r.u32(); err != nil {
			return status, fmt.Errorf("read battlefield shutdown timer: %w", err)
		}
		if status.Time2, err = r.u32(); err != nil {
			return status, fmt.Errorf("read battlefield start timer: %w", err)
		}
		if status.ArenaFaction, err = r.u8(); err != nil {
			return status, fmt.Errorf("read battlefield faction: %w", err)
		}
	default:
		return status, fmt.Errorf("battlefield status %d is unknown", status.Status)
	}
	if err := finishReader(&r, "battlefield status"); err != nil {
		return status, err
	}
	return status, nil
}

type legacyBattlefieldList struct {
	Battlemaster   uint64
	FromUI         bool
	BgTypeID       uint32
	MinLevel       byte
	MaxLevel       byte
	RandomWinToday bool
	Instances      []uint32
}

func parseLegacyBattlefieldList(body []byte) (legacyBattlefieldList, error) {
	r := movementReader{data: body}
	var list legacyBattlefieldList
	guid, err := r.u64()
	if err != nil {
		return list, fmt.Errorf("read battlefield list master: %w", err)
	}
	list.Battlemaster = guid
	fromWhere, err := r.u8()
	if err != nil {
		return list, fmt.Errorf("read battlefield list origin: %w", err)
	}
	list.FromUI = fromWhere != 0
	if list.BgTypeID, err = r.u32(); err != nil {
		return list, fmt.Errorf("read battlefield list type: %w", err)
	}
	if list.MinLevel, err = r.u8(); err != nil {
		return list, fmt.Errorf("read battlefield list min level: %w", err)
	}
	if list.MaxLevel, err = r.u8(); err != nil {
		return list, fmt.Errorf("read battlefield list max level: %w", err)
	}
	if _, err = r.u8(); err != nil {
		return list, fmt.Errorf("read battlefield list win flag: %w", err)
	}
	for range 3 {
		if _, err = r.u32(); err != nil {
			return list, fmt.Errorf("read battlefield list reward: %w", err)
		}
	}
	random, err := r.u8()
	if err != nil {
		return list, fmt.Errorf("read battlefield list random flag: %w", err)
	}
	if random != 0 {
		won, readErr := r.u8()
		if readErr != nil {
			return list, fmt.Errorf("read battlefield list random win: %w", readErr)
		}
		list.RandomWinToday = won != 0
		for range 3 {
			if _, err = r.u32(); err != nil {
				return list, fmt.Errorf("read battlefield list random reward: %w", err)
			}
		}
	}
	count, err := r.u32()
	if err != nil {
		return list, fmt.Errorf("read battlefield list count: %w", err)
	}
	if count > battlegroundMaxInstances {
		return list, fmt.Errorf("battlefield list count %d is too large", count)
	}
	list.Instances = make([]uint32, count)
	for index := range list.Instances {
		instance, readErr := r.u32()
		if readErr != nil {
			return list, fmt.Errorf("read battlefield instance: %w", readErr)
		}
		list.Instances[index] = instance
	}
	if err := finishReader(&r, "battlefield list"); err != nil {
		return list, err
	}
	return list, nil
}

type legacyPVPPlayer struct {
	GUID    uint64
	Kills   uint32
	Faction bool
	Honor   *[3]uint32
	Damage  uint32
	Healing uint32
	Stats   []uint32
}

type legacyPVPLog struct {
	Arena   bool
	Names   [2]string
	Winner  *byte
	Players []legacyPVPPlayer
	Counts  [2]int8
}

func parseLegacyPVPLog(body []byte) (legacyPVPLog, error) {
	r := movementReader{data: body}
	var log legacyPVPLog
	kind, err := r.u8()
	if err != nil {
		return log, fmt.Errorf("read pvp log type: %w", err)
	}
	if kind > 1 {
		return log, fmt.Errorf("pvp log type %d is unknown", kind)
	}
	log.Arena = kind == 1
	if log.Arena {
		for index := 0; index < 2; index++ {
			for range 3 {
				if _, err = r.u32(); err != nil {
					return log, fmt.Errorf("read arena rating: %w", err)
				}
			}
		}
		for index := 0; index < 2; index++ {
			name, readErr := r.cstring()
			if readErr != nil {
				return log, fmt.Errorf("read arena team name: %w", readErr)
			}
			log.Names[index] = truncateWireString(name, 127)
		}
	}
	ended, err := r.u8()
	if err != nil {
		return log, fmt.Errorf("read pvp log ended: %w", err)
	}
	if ended != 0 {
		winner, readErr := r.u8()
		if readErr != nil {
			return log, fmt.Errorf("read pvp log winner: %w", readErr)
		}
		log.Winner = &winner
	}
	count, err := r.u32()
	if err != nil {
		return log, fmt.Errorf("read pvp log count: %w", err)
	}
	if count > battlegroundMaxPlayers {
		return log, fmt.Errorf("pvp log count %d is too large", count)
	}
	log.Players = make([]legacyPVPPlayer, count)
	for index := range log.Players {
		player, readErr := readLegacyPVPPlayer(&r, log.Arena)
		if readErr != nil {
			return log, readErr
		}
		if log.Arena {
			side := 0
			if player.Faction {
				side = 1
			}
			if log.Counts[side] < 127 {
				log.Counts[side]++
			}
		}
		log.Players[index] = player
	}
	if err := finishReader(&r, "pvp log"); err != nil {
		return log, err
	}
	return log, nil
}

func readLegacyPVPPlayer(r *movementReader, arena bool) (legacyPVPPlayer, error) {
	var player legacyPVPPlayer
	guid, err := r.u64()
	if err != nil {
		return player, fmt.Errorf("read pvp player: %w", err)
	}
	player.GUID = guid
	if player.Kills, err = r.u32(); err != nil {
		return player, fmt.Errorf("read pvp kills: %w", err)
	}
	if arena {
		team, readErr := r.u8()
		if readErr != nil {
			return player, fmt.Errorf("read pvp team: %w", readErr)
		}
		player.Faction = team != 0
	} else {
		honor := [3]uint32{}
		if honor[0], err = r.u32(); err != nil {
			return player, fmt.Errorf("read pvp honor kills: %w", err)
		}
		if honor[1], err = r.u32(); err != nil {
			return player, fmt.Errorf("read pvp deaths: %w", err)
		}
		if honor[2], err = r.u32(); err != nil {
			return player, fmt.Errorf("read pvp bonus honor: %w", err)
		}
		player.Honor = &honor
	}
	if player.Damage, err = r.u32(); err != nil {
		return player, fmt.Errorf("read pvp damage: %w", err)
	}
	if player.Healing, err = r.u32(); err != nil {
		return player, fmt.Errorf("read pvp healing: %w", err)
	}
	objectives, err := r.u32()
	if err != nil {
		return player, fmt.Errorf("read pvp objective count: %w", err)
	}
	if objectives > battlegroundMaxObjectives {
		return player, fmt.Errorf("pvp objective count %d is too large", objectives)
	}
	player.Stats = make([]uint32, objectives)
	for index := range player.Stats {
		value, readErr := r.u32()
		if readErr != nil {
			return player, fmt.Errorf("read pvp objective: %w", readErr)
		}
		player.Stats[index] = value
	}
	return player, nil
}

type legacyFlagCarrier struct {
	GUID uint64
	X    float32
	Y    float32
}

func parseLegacyBattlegroundPositions(body []byte) ([]legacyFlagCarrier, error) {
	r := movementReader{data: body}
	members, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read battleground member count: %w", err)
	}
	carriers, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read flag carrier count: %w", err)
	}
	if members > battlegroundMaxPositions || carriers > battlegroundMaxPositions {
		return nil, fmt.Errorf("battleground positions %d/%d are too large", members, carriers)
	}
	for range members {
		if _, err = readLegacyFlagPosition(&r); err != nil {
			return nil, err
		}
	}
	result := make([]legacyFlagCarrier, carriers)
	for index := range result {
		carrier, readErr := readLegacyFlagPosition(&r)
		if readErr != nil {
			return nil, readErr
		}
		result[index] = carrier
	}
	if err := finishReader(&r, "battleground positions"); err != nil {
		return nil, err
	}
	return result, nil
}

func readLegacyFlagPosition(r *movementReader) (legacyFlagCarrier, error) {
	var carrier legacyFlagCarrier
	guid, err := r.u64()
	if err != nil {
		return carrier, fmt.Errorf("read battleground position player: %w", err)
	}
	carrier.GUID = guid
	if carrier.X, err = r.f32(); err != nil {
		return carrier, fmt.Errorf("read battleground position x: %w", err)
	}
	if carrier.Y, err = r.f32(); err != nil {
		return carrier, fmt.Errorf("read battleground position y: %w", err)
	}
	return carrier, nil
}

func parseLegacyGUIDPacket(body []byte, what string) (uint64, error) {
	r := movementReader{data: body}
	guid, err := r.u64()
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", what, err)
	}
	if err := finishReader(&r, what); err != nil {
		return 0, err
	}
	return guid, nil
}

func appendBattlegroundTicket(dst []byte, requester GUID128, id uint32, when int64) []byte {
	dst = appendPackedGUID128(dst, requester.Low, requester.High)
	dst = binary.LittleEndian.AppendUint32(dst, id)
	dst = binary.LittleEndian.AppendUint32(dst, battlegroundRideType)
	dst = binary.LittleEndian.AppendUint64(dst, uint64(when))
	return appendBits(dst, false)
}

func appendBattlegroundHeader(dst []byte, requester GUID128, id uint32, when int64, status legacyBattlefieldStatus) []byte {
	dst = appendBattlegroundTicket(dst, requester, id, when)
	dst = binary.LittleEndian.AppendUint32(dst, 1)
	dst = append(dst, status.MinLevel, status.MaxLevel, status.ArenaType)
	dst = binary.LittleEndian.AppendUint32(dst, status.InstanceID)
	dst = binary.LittleEndian.AppendUint64(dst, uint64(status.BgTypeID)|battlegroundQueueMask)
	return appendBits(dst, status.Rated, false)
}

func appendPVPLogHeader(arena bool, winner bool, names [2]string) []byte {
	bits := newBitWriter(nil)
	bits.writeBit(false)
	bits.writeBit(arena)
	bits.writeBit(winner)
	if arena {
		for _, name := range names {
			bits.writeBits(uint32(len(name)), 7)
		}
	}
	dst := bits.flush()
	if !arena {
		return dst
	}
	for _, name := range names {
		dst = appendPackedGUID128(dst, 0, 0)
		dst = append(dst, name...)
	}
	return dst
}

func appendPVPPlayer(dst []byte, player legacyPVPPlayer, ctx BattlegroundContext) []byte {
	identity := ctx.player(player.GUID)
	race, class, sex, alliance := identity.resolved()
	faction := player.Faction
	if player.Honor != nil {
		faction = alliance
	}
	modern := ctx.guid(player.GUID)
	dst = appendPackedGUID128(dst, modern.Low, modern.High)
	dst = binary.LittleEndian.AppendUint32(dst, player.Kills)
	dst = binary.LittleEndian.AppendUint32(dst, player.Damage)
	dst = binary.LittleEndian.AppendUint32(dst, player.Healing)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(player.Stats)))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = append(dst, sex)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(race))
	dst = binary.LittleEndian.AppendUint32(dst, uint32(class))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(battlegroundHonorLevel))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	for _, stat := range player.Stats {
		dst = binary.LittleEndian.AppendUint32(dst, stat)
	}
	dst = appendBits(dst, faction, true, player.Honor != nil, false, false, false, false)
	if player.Honor != nil {
		dst = binary.LittleEndian.AppendUint32(dst, player.Honor[0])
		dst = binary.LittleEndian.AppendUint32(dst, player.Honor[1])
		dst = binary.LittleEndian.AppendUint32(dst, player.Honor[2])
	}
	return dst
}

func (player BattlegroundPlayer) resolved() (race, class, sex byte, alliance bool) {
	if !player.Known {
		return 1, 1, 0, true
	}
	race, class, sex = player.Race, player.Class, player.Sex
	if race == 0 {
		race = 1
	}
	if class == 0 {
		class = 1
	}
	return race, class, sex, FactionGroupForRace(race) != 1
}

func battlegroundFlagIcon(player BattlegroundPlayer) (int8, int8) {
	_, _, _, alliance := player.resolved()
	if alliance {
		return 1, 3
	}
	return 2, 2
}

func (ctx BattlegroundContext) guid(legacy uint64) GUID128 {
	if ctx.GUID != nil {
		return ctx.GUID(legacy)
	}
	return ModernGUIDForLegacy(legacy, 0)
}

func (ctx BattlegroundContext) player(legacy uint64) BattlegroundPlayer {
	if ctx.Player != nil {
		return ctx.Player(legacy)
	}
	return BattlegroundPlayer{}
}

func appendLegacyQueueHeader(queue BattlegroundQueue, accept bool) []byte {
	body := []byte{queue.ArenaType, queue.Marker}
	body = binary.LittleEndian.AppendUint32(body, queue.BgTypeID)
	body = binary.LittleEndian.AppendUint16(body, battlegroundPortConstant)
	action := byte(0)
	if accept {
		action = 1
	}
	return append(body, action)
}
