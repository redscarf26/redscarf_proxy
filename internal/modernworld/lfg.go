package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Random-dungeon packets.
// Legacy fields follow AzerothCore 3.3.5a LFGHandler.cpp.
// 54261 layouts follow legacy proxy Write/Read. Queue order, role-check members,
// blacklist soft-lock, completion rewards, and DF leave/set-roles do not follow Hermes.

const (
	CMSGDFJoin             = uint16(0x360B)
	CMSGDFLeave            = uint16(0x3614)
	CMSGDFProposalResponse = uint16(0x3609)
	CMSGDFBootPlayerVote   = uint16(0x3618)
	CMSGDFTeleport         = uint16(0x3619)
	SMSGLFGDisabled        = uint16(0x2A33)
	SMSGLFGJoinResult      = uint16(0x2A1C)
	SMSGLFGOfferContinue   = uint16(0x2A34)
	SMSGLFGPartyInfo       = uint16(0x2A36)
	SMSGLFGPlayerInfo      = uint16(0x2A37)
	SMSGLFGPlayerReward    = uint16(0x2A38)
	SMSGLFGProposalUpdate  = uint16(0x2A2D)
	SMSGLFGQueueStatus     = uint16(0x2A20)
	SMSGLFGRoleCheckUpdate = uint16(0x2A21)
	SMSGLFGUpdateStatus    = uint16(0x2A24)
	SMSGRoleChosen         = uint16(0x2A39)
	SMSGLFGTeleportDenied  = uint16(0x2A32)

	LFGQueueIdle   byte = 0
	LFGQueuePlayer byte = 1
	LFGQueueParty  byte = 2

	lfgRideType               = uint32(2)
	lfgTicketID               = uint32(1)
	lfgUpdateReason           = byte(5)
	lfgUpdateRemovedFromQueue = byte(7)
	lfgMaxList                = 64
	lfgMaxLocks               = 128
)

// LFGTicket is the 54261 ride ticket legacy proxy writes for LFG packets. Ride type is LFG.
type LFGTicket struct {
	Requester GUID128
	ID        uint32
	Time      int64
}

// LFGContext carries the session values legacy proxy keeps beside the legacy packet.
type LFGContext struct {
	Ticket         LFGTicket
	RequestedRoles byte
	GUID           func(uint64) GUID128
}

// LegacyLFGUpdate is one SMSG_LFG_UPDATE_PLAYER or SMSG_LFG_UPDATE_PARTY.
type LegacyLFGUpdate struct {
	UpdateType byte
	HasExtra   bool
	Join       bool
	Queued     bool
	Slots      []uint32
}

// LFGJoinRequest is the part of CMSG_DF_JOIN the legacy server accepts.
type LFGJoinRequest struct {
	Roles byte
	Slots []uint32
}

// LFGProposalResponse is CMSG_DF_PROPOSAL_RESPONSE with the ticket and instance id removed.
type LFGProposalResponse struct {
	ProposalID uint32
	Accept     bool
}

type lfgItem struct {
	ID       uint32
	Quantity uint32
}

type lfgLock struct {
	Slot   uint32
	Reason uint32
}

type lfgPartyLock struct {
	GUID  uint64
	Locks []lfgLock
}

type lfgDungeon struct {
	Slot  uint32
	Done  bool
	Money uint32
	XP    uint32
	Items []lfgItem
}

type lfgRoleMember struct {
	GUID  uint64
	Ready bool
	Roles uint32
	Level byte
}

type lfgProposalPlayer struct {
	Roles     uint32
	Me        bool
	MyParty   bool
	SameParty bool
	Responded bool
	Accepted  bool
}

// NewLFGTicket builds a ticket for the current player. The id stays 1; callers pass the clock.
func NewLFGTicket(requester GUID128, unixSeconds int64) LFGTicket {
	return LFGTicket{Requester: requester, ID: lfgTicketID, Time: unixSeconds}
}

// NextLFGQueueMode reproduces legacy proxy's session mode byte.
// A player update with dungeon info selects player mode and a party update selects party mode.
// Leaving the queue clears the mode. The packet for the other mode is not sent, except that
// a leave (update type 7) is sent because the clear happens before the check.
func NextLFGQueueMode(mode byte, party bool, updateType byte, hasExtra bool) (byte, bool) {
	next := mode
	if hasExtra {
		next = LFGQueuePlayer
		if party {
			next = LFGQueueParty
		}
	}
	if updateType == lfgUpdateRemovedFromQueue {
		next = LFGQueueIdle
	}
	if party {
		return next, next != LFGQueuePlayer
	}
	return next, next != LFGQueueParty
}

func (ctx LFGContext) guid(legacy uint64) GUID128 {
	if ctx.GUID != nil {
		return ctx.GUID(legacy)
	}
	return ModernGUIDForLegacy(legacy, 0)
}

func ParseDFJoin(body []byte) (LFGJoinRequest, error) {
	var request LFGJoinRequest
	r := movementReader{data: body}
	if _, err := r.bit(); err != nil {
		return request, fmt.Errorf("read DF join group bit: %w", err)
	}
	hasParty, err := r.bit()
	if err != nil {
		return request, fmt.Errorf("read DF join party bit: %w", err)
	}
	if _, err = r.bit(); err != nil {
		return request, fmt.Errorf("read DF join unknown bit: %w", err)
	}
	if request.Roles, err = r.u8(); err != nil {
		return request, fmt.Errorf("read DF join roles: %w", err)
	}
	count, err := r.u32()
	if err != nil {
		return request, fmt.Errorf("read DF join slot count: %w", err)
	}
	if hasParty {
		if _, err = r.u8(); err != nil {
			return request, fmt.Errorf("read DF join party index: %w", err)
		}
	}
	if request.Slots, err = r.u32List(count, "DF join slots"); err != nil {
		return request, err
	}
	if err = finishReader(&r, "DF join"); err != nil {
		return request, err
	}
	return request, nil
}

// EncodeLegacyLFGJoin writes CMSG_LFG_JOIN: roles widened to uint32, two zero bytes,
// the slot list, a needs count of 3 with three zeros, and an empty comment.
func EncodeLegacyLFGJoin(request LFGJoinRequest) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(request.Roles))
	body = append(body, 0, 0, byte(len(request.Slots)))
	for _, slot := range request.Slots {
		body = binary.LittleEndian.AppendUint32(body, slot)
	}
	return append(body, 3, 0, 0, 0, 0)
}

func ParseDFLeave(body []byte) error {
	r := movementReader{data: body}
	if err := r.rideTicket(); err != nil {
		return fmt.Errorf("read DF leave ticket: %w", err)
	}
	return finishReader(&r, "DF leave")
}

func ParseDFProposalResponse(body []byte) (LFGProposalResponse, error) {
	var response LFGProposalResponse
	r := movementReader{data: body}
	if err := r.rideTicket(); err != nil {
		return response, fmt.Errorf("read DF proposal ticket: %w", err)
	}
	if _, err := r.u64(); err != nil {
		return response, fmt.Errorf("read DF proposal instance: %w", err)
	}
	var err error
	if response.ProposalID, err = r.u32(); err != nil {
		return response, fmt.Errorf("read DF proposal id: %w", err)
	}
	if response.Accept, err = r.bit(); err != nil {
		return response, fmt.Errorf("read DF proposal accept: %w", err)
	}
	if err = finishReader(&r, "DF proposal response"); err != nil {
		return response, err
	}
	return response, nil
}

func EncodeLegacyLFGProposalResult(response LFGProposalResponse) []byte {
	body := binary.LittleEndian.AppendUint32(nil, response.ProposalID)
	if response.Accept {
		return append(body, 1)
	}
	return append(body, 0)
}

// ParseDFSetRoles reads the party-index bit, the role byte, and an optional index.
// The index is discarded. The role byte is not masked: leader-only and zero are valid.
func ParseDFSetRoles(body []byte) (byte, error) {
	r := movementReader{data: body}
	hasParty, err := r.bit()
	if err != nil {
		return 0, fmt.Errorf("read DF set-roles party bit: %w", err)
	}
	roles, err := r.u8()
	if err != nil {
		return 0, fmt.Errorf("read DF set-roles: %w", err)
	}
	if hasParty {
		if _, err = r.u8(); err != nil {
			return 0, fmt.Errorf("read DF set-roles party index: %w", err)
		}
	}
	if err = finishReader(&r, "DF set-roles"); err != nil {
		return 0, err
	}
	return roles, nil
}

// ParseDFGetSystemInfo reads the player bit. True asks for the player's own locks.
func ParseDFGetSystemInfo(body []byte) (bool, error) {
	r := movementReader{data: body}
	player, err := r.bit()
	if err != nil {
		return false, fmt.Errorf("read DF system info: %w", err)
	}
	if err = finishReader(&r, "DF system info"); err != nil {
		return false, err
	}
	return player, nil
}

func ParseDFGetJoinStatus(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("DF join status has %d bytes, want 0", len(body))
	}
	return nil
}

func ParseDFBootPlayerVote(body []byte) (bool, error) {
	return parseOneBit(body, "DF boot vote")
}

func ParseDFTeleport(body []byte) (bool, error) {
	return parseOneBit(body, "DF teleport")
}

func parseOneBit(body []byte, what string) (bool, error) {
	r := movementReader{data: body}
	value, err := r.bit()
	if err != nil {
		return false, fmt.Errorf("read %s: %w", what, err)
	}
	if err = finishReader(&r, what); err != nil {
		return false, err
	}
	return value, nil
}

func TranslateLegacyLFGDisabled(body []byte) ([]byte, error) {
	if len(body) != 0 {
		return nil, fmt.Errorf("LFG disabled has %d bytes, want 0", len(body))
	}
	return nil, nil
}

func TranslateLegacyLFGOfferContinue(body []byte) ([]byte, error) {
	if len(body) != 4 {
		return nil, fmt.Errorf("LFG offer continue has %d bytes, want 4", len(body))
	}
	return append([]byte(nil), body...), nil
}

func TranslateLegacyLFGPlayerInfo(body []byte) ([]byte, error) {
	r := movementReader{data: body}
	count, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG player-info dungeon count: %w", err)
	}
	if int(count) > lfgMaxList {
		return nil, fmt.Errorf("LFG player-info dungeon count %d is too large", count)
	}
	dungeons := make([]lfgDungeon, 0, count)
	for index := 0; index < int(count); index++ {
		dungeon, err := r.playerDungeon()
		if err != nil {
			return nil, err
		}
		dungeons = append(dungeons, dungeon)
	}
	locks, err := r.playerLocks()
	if err != nil {
		return nil, err
	}
	if err = finishReader(&r, "LFG player info"); err != nil {
		return nil, err
	}
	encoded := binary.LittleEndian.AppendUint32(nil, uint32(len(dungeons)))
	encoded = appendBlacklist(encoded, nil, locks)
	for _, dungeon := range dungeons {
		encoded = appendPlayerDungeon(encoded, dungeon)
	}
	return encoded, nil
}

func TranslateLegacyLFGPartyInfo(body []byte, ctx LFGContext) ([]byte, error) {
	r := movementReader{data: body}
	locks, err := r.partyLocks()
	if err != nil {
		return nil, err
	}
	if err = finishReader(&r, "LFG party info"); err != nil {
		return nil, err
	}
	return appendPartyLocks(nil, locks, ctx), nil
}

func TranslateLegacyLFGJoinResult(body []byte, ctx LFGContext) ([]byte, error) {
	r := movementReader{data: body}
	result, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG join result: %w", err)
	}
	state, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG join state: %w", err)
	}
	resultByte, err := narrowByte(result, "LFG join result")
	if err != nil {
		return nil, err
	}
	stateByte, err := narrowByte(state, "LFG join state")
	if err != nil {
		return nil, err
	}
	var locks []lfgPartyLock
	if r.remaining() > 0 {
		if locks, err = r.partyLocks(); err != nil {
			return nil, err
		}
	}
	if err = finishReader(&r, "LFG join result"); err != nil {
		return nil, err
	}
	encoded := appendLFGTicket(nil, ctx.Ticket)
	encoded = append(encoded, resultByte, stateByte)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(locks)))
	encoded = binary.LittleEndian.AppendUint32(encoded, 0)
	return append(encoded, encodePartyLocks(locks, ctx)...), nil
}

func TranslateLegacyLFGQueueStatus(body []byte, ctx LFGContext) ([]byte, error) {
	r := movementReader{data: body}
	slot, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG queue dungeon: %w", err)
	}
	average, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG queue average wait: %w", err)
	}
	mine, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG queue wait: %w", err)
	}
	tankWait, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG queue tank wait: %w", err)
	}
	healerWait, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG queue healer wait: %w", err)
	}
	dpsWait, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG queue dps wait: %w", err)
	}
	tanks, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG queue tanks: %w", err)
	}
	healers, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG queue healers: %w", err)
	}
	dps, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG queue dps: %w", err)
	}
	queued, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG queue time: %w", err)
	}
	if err = finishReader(&r, "LFG queue status"); err != nil {
		return nil, err
	}
	encoded := appendLFGTicket(nil, ctx.Ticket)
	encoded = binary.LittleEndian.AppendUint32(encoded, slot)
	encoded = binary.LittleEndian.AppendUint32(encoded, average)
	encoded = binary.LittleEndian.AppendUint32(encoded, mine)
	encoded = binary.LittleEndian.AppendUint32(encoded, tankWait)
	encoded = append(encoded, tanks)
	encoded = binary.LittleEndian.AppendUint32(encoded, healerWait)
	encoded = append(encoded, healers)
	encoded = binary.LittleEndian.AppendUint32(encoded, dpsWait)
	encoded = append(encoded, dps)
	encoded = binary.LittleEndian.AppendUint32(encoded, queued)
	return encoded, nil
}

func TranslateLegacyLFGRoleCheckUpdate(body []byte, ctx LFGContext) ([]byte, error) {
	r := movementReader{data: body}
	state, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG role-check state: %w", err)
	}
	status, err := narrowByte(state, "LFG role-check state")
	if err != nil {
		return nil, err
	}
	beginning, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG role-check beginning: %w", err)
	}
	dungeonCount, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG role-check dungeon count: %w", err)
	}
	slots, err := r.u32List(uint32(dungeonCount), "LFG role-check dungeons")
	if err != nil {
		return nil, err
	}
	memberCount, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG role-check member count: %w", err)
	}
	if int(memberCount) > lfgMaxList {
		return nil, fmt.Errorf("LFG role-check member count %d is too large", memberCount)
	}
	members := make([]lfgRoleMember, 0, memberCount)
	for index := 0; index < int(memberCount); index++ {
		member, err := r.roleMember()
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	if err = finishReader(&r, "LFG role check"); err != nil {
		return nil, err
	}
	encoded := []byte{0, status}
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(slots)))
	encoded = binary.LittleEndian.AppendUint32(encoded, 0)
	encoded = binary.LittleEndian.AppendUint32(encoded, 0)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(members)))
	for _, slot := range slots {
		encoded = binary.LittleEndian.AppendUint32(encoded, slot)
	}
	encoded = appendBits(encoded, beginning != 0, false)
	for _, member := range members {
		guid := ctx.guid(member.GUID)
		encoded = appendPackedGUID128(encoded, guid.Low, guid.High)
		encoded = append(encoded, byte(member.Roles), member.Level)
		encoded = appendBits(encoded, member.Ready)
	}
	return encoded, nil
}

func TranslateLegacyLFGProposalUpdate(body []byte, ctx LFGContext) ([]byte, error) {
	r := movementReader{data: body}
	slot, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG proposal dungeon: %w", err)
	}
	state, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG proposal state: %w", err)
	}
	proposalID, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG proposal id: %w", err)
	}
	completed, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG proposal encounters: %w", err)
	}
	silent, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG proposal silent: %w", err)
	}
	count, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG proposal player count: %w", err)
	}
	if int(count) > lfgMaxList {
		return nil, fmt.Errorf("LFG proposal player count %d is too large", count)
	}
	players := make([]lfgProposalPlayer, 0, count)
	for index := 0; index < int(count); index++ {
		player, err := r.proposalPlayer()
		if err != nil {
			return nil, err
		}
		players = append(players, player)
	}
	if err = finishReader(&r, "LFG proposal"); err != nil {
		return nil, err
	}
	encoded := appendLFGTicket(nil, ctx.Ticket)
	encoded = binary.LittleEndian.AppendUint64(encoded, 0)
	encoded = binary.LittleEndian.AppendUint32(encoded, proposalID)
	encoded = binary.LittleEndian.AppendUint32(encoded, slot)
	encoded = append(encoded, state)
	encoded = binary.LittleEndian.AppendUint32(encoded, completed)
	encoded = binary.LittleEndian.AppendUint32(encoded, 0)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(players)))
	encoded = append(encoded, 0)
	// ValidCompletedMask is set so the encounter mask is used. IsRequeue stays clear.
	encoded = appendBits(encoded, true, silent != 0, false)
	for _, player := range players {
		encoded = append(encoded, byte(player.Roles))
		encoded = appendBits(encoded, player.Me, player.SameParty, player.MyParty, player.Responded, player.Accepted)
	}
	return encoded, nil
}

func TranslateLegacyLFGPlayerReward(body []byte) ([]byte, error) {
	r := movementReader{data: body}
	randomDungeon, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG reward random dungeon: %w", err)
	}
	dungeon, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG reward dungeon: %w", err)
	}
	if _, err = r.u8(); err != nil {
		return nil, fmt.Errorf("read LFG reward done: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return nil, fmt.Errorf("read LFG reward unknown: %w", err)
	}
	money, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG reward money: %w", err)
	}
	xp, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG reward xp: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return nil, fmt.Errorf("read LFG reward padding: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return nil, fmt.Errorf("read LFG reward padding: %w", err)
	}
	items, err := r.rewardItems()
	if err != nil {
		return nil, err
	}
	if err = finishReader(&r, "LFG player reward"); err != nil {
		return nil, err
	}
	encoded := binary.LittleEndian.AppendUint32(nil, randomDungeon)
	encoded = binary.LittleEndian.AppendUint32(encoded, dungeon)
	encoded = binary.LittleEndian.AppendUint32(encoded, money)
	encoded = binary.LittleEndian.AppendUint32(encoded, xp)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(items)))
	for _, item := range items {
		encoded = appendCompletionReward(encoded, item)
	}
	return encoded, nil
}

func TranslateLegacyLFGUpdate(body []byte, party bool, ctx LFGContext) ([]byte, LegacyLFGUpdate, error) {
	parsed, err := parseLegacyLFGUpdate(body, party)
	if err != nil {
		return nil, LegacyLFGUpdate{}, err
	}
	return encodeLFGUpdateStatus(parsed, party, ctx), parsed, nil
}

func TranslateLegacyLFGRoleChosen(body []byte, ctx LFGContext) ([]byte, error) {
	r := movementReader{data: body}
	guid, err := r.u64()
	if err != nil {
		return nil, fmt.Errorf("read LFG role-chosen guid: %w", err)
	}
	ready, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG role-chosen ready: %w", err)
	}
	roles, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG role-chosen roles: %w", err)
	}
	if err = finishReader(&r, "LFG role chosen"); err != nil {
		return nil, err
	}
	modern := ctx.guid(guid)
	encoded := appendPackedGUID128(nil, modern.Low, modern.High)
	encoded = append(encoded, byte(roles))
	return appendBits(encoded, ready != 0), nil
}

func TranslateLegacyLFGTeleportDenied(body []byte) ([]byte, error) {
	if len(body) != 4 {
		return nil, fmt.Errorf("LFG teleport denied has %d bytes, want 4", len(body))
	}
	reason := binary.LittleEndian.Uint32(body)
	if reason > 15 {
		return nil, fmt.Errorf("LFG teleport reason %d does not fit in 4 bits", reason)
	}
	bits := newBitWriter(nil)
	bits.writeBits(reason, 4)
	return bits.flush(), nil
}

func parseLegacyLFGUpdate(body []byte, party bool) (LegacyLFGUpdate, error) {
	var update LegacyLFGUpdate
	r := movementReader{data: body}
	var err error
	if update.UpdateType, err = r.u8(); err != nil {
		return update, fmt.Errorf("read LFG update type: %w", err)
	}
	extra, err := r.u8()
	if err != nil {
		return update, fmt.Errorf("read LFG update extra: %w", err)
	}
	update.HasExtra = extra != 0
	if update.HasExtra {
		if party {
			join, err := r.u8()
			if err != nil {
				return update, fmt.Errorf("read LFG party join: %w", err)
			}
			update.Join = join != 0
		}
		queued, err := r.u8()
		if err != nil {
			return update, fmt.Errorf("read LFG update queued: %w", err)
		}
		update.Queued = queued != 0
		unknowns := 2
		if party {
			unknowns = 5
		}
		for index := 0; index < unknowns; index++ {
			if _, err = r.u8(); err != nil {
				return update, fmt.Errorf("read LFG update padding: %w", err)
			}
		}
		count, err := r.u8()
		if err != nil {
			return update, fmt.Errorf("read LFG update slot count: %w", err)
		}
		if update.Slots, err = r.u32List(uint32(count), "LFG update slots"); err != nil {
			return update, err
		}
		if _, err = readLegacyCString(&r, 1024); err != nil {
			return update, fmt.Errorf("read LFG update comment: %w", err)
		}
	}
	if err = finishReader(&r, "LFG update"); err != nil {
		return update, err
	}
	return update, nil
}

func encodeLFGUpdateStatus(update LegacyLFGUpdate, party bool, ctx LFGContext) []byte {
	encoded := appendLFGTicket(nil, ctx.Ticket)
	encoded = append(encoded, update.UpdateType, lfgUpdateReason)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(update.Slots)))
	encoded = append(encoded, ctx.RequestedRoles)
	encoded = binary.LittleEndian.AppendUint32(encoded, 0)
	encoded = binary.LittleEndian.AppendUint32(encoded, 0)
	for _, slot := range update.Slots {
		encoded = binary.LittleEndian.AppendUint32(encoded, slot)
	}
	joined := update.HasExtra && update.Queued
	lfgJoined := party && update.HasExtra && update.Join
	queued := update.HasExtra && update.Queued
	return appendBits(encoded, false, true, joined, lfgJoined, queued, false)
}

func appendPartyLocks(dst []byte, locks []lfgPartyLock, ctx LFGContext) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(locks)))
	return append(dst, encodePartyLocks(locks, ctx)...)
}

func encodePartyLocks(locks []lfgPartyLock, ctx LFGContext) []byte {
	var encoded []byte
	for _, lock := range locks {
		guid := ctx.guid(lock.GUID)
		encoded = appendBlacklist(encoded, &guid, lock.Locks)
	}
	return encoded
}

func appendBlacklist(dst []byte, guid *GUID128, locks []lfgLock) []byte {
	dst = appendBits(dst, guid != nil)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(locks)))
	if guid != nil {
		dst = appendPackedGUID128(dst, guid.Low, guid.High)
	}
	for _, lock := range locks {
		dst = binary.LittleEndian.AppendUint32(dst, lock.Slot)
		dst = binary.LittleEndian.AppendUint32(dst, lock.Reason)
		dst = binary.LittleEndian.AppendUint32(dst, 0)
		dst = binary.LittleEndian.AppendUint32(dst, 0)
		dst = binary.LittleEndian.AppendUint32(dst, 0)
	}
	return dst
}

func appendPlayerDungeon(dst []byte, dungeon lfgDungeon) []byte {
	slot := dungeon.Slot &^ (1 << 31)
	completionQuantity := uint32(0)
	if dungeon.Slot&(1<<31) != 0 {
		completionQuantity = 1
	}
	fields := [...]uint32{
		slot,
		completionQuantity,
		1,
		0,
		0,
		1,
		0,
		1,
		0,
		0,
		0,
		0,
		1,
		0,
		0,
		0,
	}
	for _, field := range fields {
		dst = binary.LittleEndian.AppendUint32(dst, field)
	}
	dst = appendBits(dst, dungeon.Done, false)
	return appendQuestReward(dst, dungeon.Money, dungeon.XP, dungeon.Items)
}

func appendQuestReward(dst []byte, money, xp uint32, items []lfgItem) []byte {
	dst = append(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, money)
	dst = binary.LittleEndian.AppendUint32(dst, xp)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(items)))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	for _, item := range items {
		dst = binary.LittleEndian.AppendUint32(dst, item.ID)
		dst = binary.LittleEndian.AppendUint32(dst, item.Quantity)
	}
	return appendBits(dst, false, false, false, false)
}

func appendCompletionReward(dst []byte, item lfgItem) []byte {
	dst = appendBits(dst, true, false)
	dst = appendItemInstance(dst, item.ID, 0, 0)
	dst = binary.LittleEndian.AppendUint32(dst, item.Quantity)
	return binary.LittleEndian.AppendUint32(dst, 0)
}

func appendLFGTicket(dst []byte, ticket LFGTicket) []byte {
	dst = appendPackedGUID128(dst, ticket.Requester.Low, ticket.Requester.High)
	dst = binary.LittleEndian.AppendUint32(dst, ticket.ID)
	dst = binary.LittleEndian.AppendUint32(dst, lfgRideType)
	dst = binary.LittleEndian.AppendUint64(dst, uint64(ticket.Time))
	return appendBits(dst, false)
}

func appendBits(dst []byte, bits ...bool) []byte {
	writer := newBitWriter(dst)
	for _, bit := range bits {
		writer.writeBit(bit)
	}
	return writer.flush()
}

func narrowByte(value uint32, what string) (byte, error) {
	if value > 255 {
		return 0, fmt.Errorf("%s %d does not fit in a byte", what, value)
	}
	return byte(value), nil
}

func finishReader(r *movementReader, what string) error {
	r.align()
	if r.offset != len(r.data) {
		return fmt.Errorf("%s has %d trailing bytes", what, len(r.data)-r.offset)
	}
	return nil
}

func (r *movementReader) rideTicket() error {
	if _, err := r.guid128(); err != nil {
		return err
	}
	if _, err := r.u32(); err != nil {
		return err
	}
	if _, err := r.u32(); err != nil {
		return err
	}
	if _, err := r.u64(); err != nil {
		return err
	}
	_, err := r.bit()
	return err
}

func (r *movementReader) u32List(count uint32, what string) ([]uint32, error) {
	if count > lfgMaxList {
		return nil, fmt.Errorf("%s count %d is too large", what, count)
	}
	values := make([]uint32, count)
	for index := range values {
		value, err := r.u32()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", what, err)
		}
		values[index] = value
	}
	return values, nil
}

func (r *movementReader) rewardItems() ([]lfgItem, error) {
	count, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG reward item count: %w", err)
	}
	if int(count) > lfgMaxList {
		return nil, fmt.Errorf("LFG reward item count %d is too large", count)
	}
	items := make([]lfgItem, 0, count)
	for index := 0; index < int(count); index++ {
		id, err := r.u32()
		if err != nil {
			return nil, fmt.Errorf("read LFG reward item: %w", err)
		}
		if _, err = r.u32(); err != nil {
			return nil, fmt.Errorf("read LFG reward display: %w", err)
		}
		quantity, err := r.u32()
		if err != nil {
			return nil, fmt.Errorf("read LFG reward quantity: %w", err)
		}
		items = append(items, lfgItem{ID: id, Quantity: quantity})
	}
	return items, nil
}

func (r *movementReader) playerDungeon() (lfgDungeon, error) {
	var dungeon lfgDungeon
	var err error
	if dungeon.Slot, err = r.u32(); err != nil {
		return dungeon, fmt.Errorf("read LFG dungeon slot: %w", err)
	}
	done, err := r.u8()
	if err != nil {
		return dungeon, fmt.Errorf("read LFG dungeon done: %w", err)
	}
	dungeon.Done = done != 0
	if dungeon.Money, err = r.u32(); err != nil {
		return dungeon, fmt.Errorf("read LFG dungeon money: %w", err)
	}
	if dungeon.XP, err = r.u32(); err != nil {
		return dungeon, fmt.Errorf("read LFG dungeon xp: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return dungeon, fmt.Errorf("read LFG dungeon padding: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return dungeon, fmt.Errorf("read LFG dungeon padding: %w", err)
	}
	if dungeon.Items, err = r.rewardItems(); err != nil {
		return dungeon, err
	}
	return dungeon, nil
}

func (r *movementReader) playerLocks() ([]lfgLock, error) {
	count, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read LFG lock count: %w", err)
	}
	if count > lfgMaxLocks {
		return nil, fmt.Errorf("LFG lock count %d is too large", count)
	}
	locks := make([]lfgLock, 0, count)
	for index := uint32(0); index < count; index++ {
		slot, err := r.u32()
		if err != nil {
			return nil, fmt.Errorf("read LFG lock slot: %w", err)
		}
		reason, err := r.u32()
		if err != nil {
			return nil, fmt.Errorf("read LFG lock reason: %w", err)
		}
		locks = append(locks, lfgLock{Slot: slot, Reason: reason})
	}
	return locks, nil
}

func (r *movementReader) partyLocks() ([]lfgPartyLock, error) {
	count, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read LFG party lock count: %w", err)
	}
	if int(count) > lfgMaxList {
		return nil, fmt.Errorf("LFG party lock count %d is too large", count)
	}
	locks := make([]lfgPartyLock, 0, count)
	for index := 0; index < int(count); index++ {
		guid, err := r.u64()
		if err != nil {
			return nil, fmt.Errorf("read LFG party lock guid: %w", err)
		}
		playerLocks, err := r.playerLocks()
		if err != nil {
			return nil, err
		}
		locks = append(locks, lfgPartyLock{GUID: guid, Locks: playerLocks})
	}
	return locks, nil
}

func (r *movementReader) roleMember() (lfgRoleMember, error) {
	var member lfgRoleMember
	var err error
	if member.GUID, err = r.u64(); err != nil {
		return member, fmt.Errorf("read LFG role-check guid: %w", err)
	}
	ready, err := r.u8()
	if err != nil {
		return member, fmt.Errorf("read LFG role-check ready: %w", err)
	}
	member.Ready = ready != 0
	if member.Roles, err = r.u32(); err != nil {
		return member, fmt.Errorf("read LFG role-check roles: %w", err)
	}
	if member.Level, err = r.u8(); err != nil {
		return member, fmt.Errorf("read LFG role-check level: %w", err)
	}
	return member, nil
}

func (r *movementReader) proposalPlayer() (lfgProposalPlayer, error) {
	var player lfgProposalPlayer
	var err error
	if player.Roles, err = r.u32(); err != nil {
		return player, fmt.Errorf("read LFG proposal role: %w", err)
	}
	flags := make([]bool, 5)
	for index := range flags {
		if flags[index], err = r.boolByte(); err != nil {
			return player, fmt.Errorf("read LFG proposal player flag: %w", err)
		}
	}
	player.Me = flags[0]
	player.MyParty = flags[1]
	player.SameParty = flags[2]
	player.Responded = flags[3]
	player.Accepted = flags[4]
	return player, nil
}

func (r *movementReader) boolByte() (bool, error) {
	value, err := r.u8()
	return value != 0, err
}
