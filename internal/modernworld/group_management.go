package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	CMSGOptOutOfLoot             = uint16(0x34F6)
	CMSGSetRole                  = uint16(0x35D9)
	CMSGInitiateRolePoll         = uint16(0x35DA)
	CMSGDFSetRoles               = uint16(0x3617)
	CMSGSetEveryoneIsAssistant   = uint16(0x361A)
	CMSGDoReadyCheck             = uint16(0x3635)
	CMSGReadyCheckResponse       = uint16(0x3636)
	CMSGPartyUninvite            = uint16(0x364A)
	CMSGSetLootMethod            = uint16(0x364B)
	CMSGSetPartyLeader           = uint16(0x364D)
	CMSGMinimapPing              = uint16(0x364E)
	CMSGChangeSubGroup           = uint16(0x364F)
	CMSGSwapSubGroups            = uint16(0x3650)
	CMSGConvertRaid              = uint16(0x3651)
	CMSGSetAssistantLeader       = uint16(0x3652)
	CMSGSetPartyAssignment       = uint16(0x3654)
	CMSGRandomRoll               = uint16(0x3657)
	SMSGRoleChangedInform        = uint16(0x258A)
	SMSGRolePollInform           = uint16(0x258B)
	SMSGReadyCheckStarted        = uint16(0x25F6)
	SMSGReadyCheckResponse       = uint16(0x25F7)
	SMSGReadyCheckCompleted      = uint16(0x25F8)
	SMSGGroupNewLeader           = uint16(0x262D)
	SMSGRandomRoll               = uint16(0x2630)
	SMSGMinimapPing              = uint16(0x26CE)
	readyCheckDurationMS         = uint64(35000)
	maximumPartyUninviteReason   = 255
	maximumLegacyPartyMemberName = 63
)

func ParseOptOutOfLoot(body []byte) (bool, error) {
	r := movementReader{data: body}
	pass, err := r.bit()
	if err != nil {
		return false, fmt.Errorf("read opt-out-of-loot flag: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return false, fmt.Errorf("opt-out-of-loot has %d trailing bytes", r.remaining())
	}
	return pass, nil
}

type PartySelector struct {
	HasParty   bool
	PartyIndex byte
}

type ReadyCheckResponseRequest struct {
	PartySelector
	Ready bool
}

type PartyUninviteRequest struct {
	PartySelector
	Target GUID128
	Reason string
}

type SetPartyLeaderRequest struct {
	PartySelector
	Target GUID128
}

type SetLootMethodRequest struct {
	PartyIndex    byte
	Method        byte
	LootMaster    GUID128
	LootThreshold uint32
}

type SetAssistantLeaderRequest struct {
	PartySelector
	Target GUID128
	Apply  bool
}

type SetEveryoneIsAssistantRequest struct {
	PartySelector
	Apply bool
}

type SetPartyAssignmentRequest struct {
	PartySelector
	Assignment byte
	Apply      bool
	Target     GUID128
}

type SetRoleRequest struct {
	PartySelector
	Target GUID128
	Role   byte
}

type ChangeSubGroupRequest struct {
	PartySelector
	Target      GUID128
	NewSubGroup byte
}

type SwapSubGroupsRequest struct {
	PartySelector
	FirstTarget  GUID128
	SecondTarget GUID128
}

type MinimapPingRequest struct {
	PartySelector
	X float32
	Y float32
}

type RandomRollRequest struct {
	PartySelector
	Minimum int32
	Maximum int32
}

type LegacyPartyMember struct {
	GUID     uint64
	Name     string
	Subgroup byte
	Flags    byte
	Roles    byte
}

type LegacyPartySnapshot struct {
	LegacyGUID    uint64
	GUID          GUID128
	PartyIndex    byte
	GroupType     byte
	IsRaid        bool
	Destroyed     bool
	Leader        uint64
	LootMethod    byte
	LootMaster    uint64
	LootThreshold byte
	Members       []LegacyPartyMember
}

func parseOptionalPartyHeader(r *movementReader) (PartySelector, error) {
	var selector PartySelector
	hasParty, err := r.bit()
	if err != nil {
		return selector, err
	}
	selector.HasParty = hasParty
	return selector, nil
}

func finishOptionalParty(r *movementReader, selector *PartySelector) error {
	if selector.HasParty {
		partyIndex, err := r.u8()
		if err != nil {
			return err
		}
		selector.PartyIndex = partyIndex
	} else {
		r.align()
	}
	if r.remaining() != 0 {
		return fmt.Errorf("packet has %d trailing bytes", r.remaining())
	}
	return nil
}

func ParseDoReadyCheck(body []byte) (PartySelector, error) {
	// 3.4.3.54261 uses an optional party-index bit. legacy proxy deliberately
	// ignores the selector because MSG_RAID_READY_CHECK has no selector.
	if len(body) == 0 {
		return PartySelector{}, nil
	}
	r := movementReader{data: body}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return selector, fmt.Errorf("read ready-check party flag: %w", err)
	}
	if err := finishOptionalParty(&r, &selector); err != nil {
		return selector, fmt.Errorf("read ready-check party index: %w", err)
	}
	return selector, nil
}

func ParseReadyCheckResponse(body []byte) (ReadyCheckResponseRequest, error) {
	var request ReadyCheckResponseRequest
	r := movementReader{data: body}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return request, fmt.Errorf("read ready-response party flag: %w", err)
	}
	request.PartySelector = selector
	if request.Ready, err = r.bit(); err != nil {
		return request, fmt.Errorf("read ready-response state: %w", err)
	}
	if err := finishOptionalParty(&r, &request.PartySelector); err != nil {
		return request, fmt.Errorf("read ready-response party index: %w", err)
	}
	return request, nil
}

func ParsePartyUninvite(body []byte) (PartyUninviteRequest, error) {
	var request PartyUninviteRequest
	r := movementReader{data: body}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return request, fmt.Errorf("read uninvite party flag: %w", err)
	}
	request.PartySelector = selector
	reasonLength, err := r.bits(8)
	if err != nil {
		return request, fmt.Errorf("read uninvite reason length: %w", err)
	}
	if reasonLength > maximumPartyUninviteReason {
		return request, fmt.Errorf("uninvite reason has %d bytes", reasonLength)
	}
	if request.Target, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read uninvite target: %w", err)
	}
	if request.HasParty {
		if request.PartyIndex, err = r.u8(); err != nil {
			return request, fmt.Errorf("read uninvite party index: %w", err)
		}
	}
	if request.Reason, err = r.stringN(int(reasonLength)); err != nil {
		return request, fmt.Errorf("read uninvite reason: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("party uninvite has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func ParseSetPartyLeader(body []byte) (SetPartyLeaderRequest, error) {
	var request SetPartyLeaderRequest
	r := movementReader{data: body}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return request, fmt.Errorf("read set-leader party flag: %w", err)
	}
	request.PartySelector = selector
	if request.Target, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read set-leader target: %w", err)
	}
	if err := finishOptionalParty(&r, &request.PartySelector); err != nil {
		return request, fmt.Errorf("read set-leader party index: %w", err)
	}
	return request, nil
}

func ParseSetLootMethod(body []byte) (SetLootMethodRequest, error) {
	var request SetLootMethodRequest
	r := movementReader{data: body}
	var err error
	// Build 54261 writes PartyIndex as a plain signed byte. In particular, -1
	// is encoded as 0xff when the UI does not select a party category. Do not
	// interpret its high bit as an optional-field marker: doing so shifts the
	// packet and makes the common 0xff form fail before it reaches the legacy
	// server, so changing the loot method appears to have no effect.
	if request.PartyIndex, err = r.u8(); err != nil {
		return request, fmt.Errorf("read loot party index: %w", err)
	}
	if request.Method, err = r.u8(); err != nil {
		return request, fmt.Errorf("read loot method: %w", err)
	}
	if request.LootMaster, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read loot master: %w", err)
	}
	if request.LootThreshold, err = r.u32(); err != nil {
		return request, fmt.Errorf("read loot threshold: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("set-loot-method has %d trailing bytes", r.remaining())
	}
	if request.Method > 4 {
		return request, fmt.Errorf("loot method %d is invalid", request.Method)
	}
	if request.LootThreshold < 2 || request.LootThreshold > 6 {
		return request, fmt.Errorf("loot threshold %d is not supported by 3.3.5", request.LootThreshold)
	}
	return request, nil
}

func ParseConvertRaid(body []byte) (bool, error) {
	r := movementReader{data: body}
	raid, err := r.bit()
	if err != nil {
		return false, fmt.Errorf("read convert-raid direction: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return false, fmt.Errorf("convert-raid has %d trailing bytes", r.remaining())
	}
	return raid, nil
}

func ParseSetAssistantLeader(body []byte) (SetAssistantLeaderRequest, error) {
	var request SetAssistantLeaderRequest
	r := movementReader{data: body}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return request, fmt.Errorf("read assistant party flag: %w", err)
	}
	request.PartySelector = selector
	if request.Apply, err = r.bit(); err != nil {
		return request, fmt.Errorf("read assistant apply flag: %w", err)
	}
	if request.Target, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read assistant target: %w", err)
	}
	if err := finishOptionalParty(&r, &request.PartySelector); err != nil {
		return request, fmt.Errorf("read assistant party index: %w", err)
	}
	return request, nil
}

func ParseSetEveryoneIsAssistant(body []byte) (SetEveryoneIsAssistantRequest, error) {
	var request SetEveryoneIsAssistantRequest
	r := movementReader{data: body}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return request, fmt.Errorf("read everyone-assistant party flag: %w", err)
	}
	request.PartySelector = selector
	if request.Apply, err = r.bit(); err != nil {
		return request, fmt.Errorf("read everyone-assistant apply flag: %w", err)
	}
	if err := finishOptionalParty(&r, &request.PartySelector); err != nil {
		return request, fmt.Errorf("read everyone-assistant party index: %w", err)
	}
	return request, nil
}

func ParseSetPartyAssignment(body []byte) (SetPartyAssignmentRequest, error) {
	var request SetPartyAssignmentRequest
	r := movementReader{data: body}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return request, fmt.Errorf("read assignment party flag: %w", err)
	}
	request.PartySelector = selector
	if request.Apply, err = r.bit(); err != nil {
		return request, fmt.Errorf("read assignment apply flag: %w", err)
	}
	if request.Assignment, err = r.u8(); err != nil {
		return request, fmt.Errorf("read assignment type: %w", err)
	}
	if request.Target, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read assignment target: %w", err)
	}
	if err := finishOptionalParty(&r, &request.PartySelector); err != nil {
		return request, fmt.Errorf("read assignment party index: %w", err)
	}
	if request.Assignment > 1 {
		return request, fmt.Errorf("assignment %d is invalid", request.Assignment)
	}
	return request, nil
}

func ParseInitiateRolePoll(body []byte) (PartySelector, error) {
	r := movementReader{data: body}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return selector, fmt.Errorf("read role-poll party flag: %w", err)
	}
	if err := finishOptionalParty(&r, &selector); err != nil {
		return selector, fmt.Errorf("read role-poll party index: %w", err)
	}
	return selector, nil
}

func ParseSetRole(body []byte) (SetRoleRequest, error) {
	var request SetRoleRequest
	r := movementReader{data: body}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return request, fmt.Errorf("read set-role party flag: %w", err)
	}
	request.PartySelector = selector
	if request.Target, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read set-role target: %w", err)
	}
	if request.Role, err = r.u8(); err != nil {
		return request, fmt.Errorf("read role: %w", err)
	}
	if err := finishOptionalParty(&r, &request.PartySelector); err != nil {
		return request, fmt.Errorf("read set-role party index: %w", err)
	}
	if request.Role&^byte(0x0e) != 0 {
		return request, fmt.Errorf("role mask 0x%x is invalid", request.Role)
	}
	return request, nil
}

func ParseChangeSubGroup(body []byte) (ChangeSubGroupRequest, error) {
	var request ChangeSubGroupRequest
	r := movementReader{data: body}
	var err error
	if request.Target, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read subgroup target: %w", err)
	}
	if request.NewSubGroup, err = r.u8(); err != nil {
		return request, fmt.Errorf("read subgroup number: %w", err)
	}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return request, fmt.Errorf("read subgroup party flag: %w", err)
	}
	request.PartySelector = selector
	if err := finishOptionalParty(&r, &request.PartySelector); err != nil {
		return request, fmt.Errorf("read subgroup party index: %w", err)
	}
	if request.NewSubGroup > 7 {
		return request, fmt.Errorf("subgroup %d is invalid", request.NewSubGroup)
	}
	return request, nil
}

func ParseSwapSubGroups(body []byte) (SwapSubGroupsRequest, error) {
	var request SwapSubGroupsRequest
	r := movementReader{data: body}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return request, fmt.Errorf("read subgroup-swap party flag: %w", err)
	}
	request.PartySelector = selector
	if request.FirstTarget, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read first subgroup-swap target: %w", err)
	}
	if request.SecondTarget, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read second subgroup-swap target: %w", err)
	}
	if err := finishOptionalParty(&r, &request.PartySelector); err != nil {
		return request, fmt.Errorf("read subgroup-swap party index: %w", err)
	}
	return request, nil
}

func ParseMinimapPing(body []byte) (MinimapPingRequest, error) {
	var request MinimapPingRequest
	r := movementReader{data: body}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return request, fmt.Errorf("read minimap-ping party flag: %w", err)
	}
	request.PartySelector = selector
	if request.X, err = r.f32(); err != nil {
		return request, fmt.Errorf("read minimap-ping X: %w", err)
	}
	if request.Y, err = r.f32(); err != nil {
		return request, fmt.Errorf("read minimap-ping Y: %w", err)
	}
	if err := finishOptionalParty(&r, &request.PartySelector); err != nil {
		return request, fmt.Errorf("read minimap-ping party index: %w", err)
	}
	if math.IsNaN(float64(request.X)) || math.IsNaN(float64(request.Y)) || math.IsInf(float64(request.X), 0) || math.IsInf(float64(request.Y), 0) {
		return request, fmt.Errorf("minimap-ping position is not finite")
	}
	return request, nil
}

func ParseRandomRoll(body []byte) (RandomRollRequest, error) {
	var request RandomRollRequest
	r := movementReader{data: body}
	selector, err := parseOptionalPartyHeader(&r)
	if err != nil {
		return request, fmt.Errorf("read random-roll party flag: %w", err)
	}
	request.PartySelector = selector
	if request.Minimum, err = r.i32(); err != nil {
		return request, fmt.Errorf("read random-roll minimum: %w", err)
	}
	if request.Maximum, err = r.i32(); err != nil {
		return request, fmt.Errorf("read random-roll maximum: %w", err)
	}
	if err := finishOptionalParty(&r, &request.PartySelector); err != nil {
		return request, fmt.Errorf("read random-roll party index: %w", err)
	}
	if request.Minimum > request.Maximum {
		return request, fmt.Errorf("random-roll minimum %d exceeds maximum %d", request.Minimum, request.Maximum)
	}
	return request, nil
}

func EncodeLegacyPartyUninvite(target uint64, reason string) []byte {
	body := binary.LittleEndian.AppendUint64(nil, target)
	reason = truncateWireString(reason, maximumPartyUninviteReason)
	body = append(body, reason...)
	return append(body, 0)
}

func EncodeLegacySetPartyLeader(target uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, target)
}

func EncodeLegacySetLootMethod(request SetLootMethodRequest, lootMaster uint64) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(request.Method))
	body = binary.LittleEndian.AppendUint64(body, lootMaster)
	return binary.LittleEndian.AppendUint32(body, request.LootThreshold)
}

func EncodeLegacySetAssistantLeader(target uint64, apply bool) []byte {
	body := binary.LittleEndian.AppendUint64(nil, target)
	return append(body, boolByte(apply))
}

func EncodeLegacySetPartyAssignment(request SetPartyAssignmentRequest, target uint64) []byte {
	body := []byte{request.Assignment, boolByte(request.Apply)}
	return binary.LittleEndian.AppendUint64(body, target)
}

func EncodeLegacyChangeSubGroup(name string, subgroup byte) []byte {
	name = truncateWireString(name, maximumLegacyPartyMemberName)
	body := append([]byte(nil), name...)
	return append(body, 0, subgroup)
}

func EncodeLegacySwapSubGroups(firstName, secondName string) []byte {
	firstName = truncateWireString(firstName, maximumLegacyPartyMemberName)
	secondName = truncateWireString(secondName, maximumLegacyPartyMemberName)
	body := append([]byte(nil), firstName...)
	body = append(body, 0)
	body = append(body, secondName...)
	return append(body, 0)
}

func EncodeLegacyMinimapPing(request MinimapPingRequest) []byte {
	body := binary.LittleEndian.AppendUint32(nil, math.Float32bits(request.X))
	return binary.LittleEndian.AppendUint32(body, math.Float32bits(request.Y))
}

func EncodeLegacyRandomRoll(request RandomRollRequest) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(request.Minimum))
	return binary.LittleEndian.AppendUint32(body, uint32(request.Maximum))
}

func EncodeReadyCheckStarted(partyIndex byte, party, initiator GUID128) []byte {
	body := []byte{partyIndex}
	body = appendPackedGUID128(body, party.Low, party.High)
	body = appendPackedGUID128(body, initiator.Low, initiator.High)
	return binary.LittleEndian.AppendUint64(body, readyCheckDurationMS)
}

func EncodeReadyCheckResponse(party, player GUID128, ready bool) []byte {
	body := appendPackedGUID128(nil, party.Low, party.High)
	body = appendPackedGUID128(body, player.Low, player.High)
	bits := newBitWriter(body)
	bits.writeBit(ready)
	return bits.flush()
}

func EncodeReadyCheckCompleted(partyIndex byte, party GUID128) []byte {
	body := []byte{partyIndex}
	return appendPackedGUID128(body, party.Low, party.High)
}

func ParseLegacyReadyCheckStarted(body []byte) (uint64, error) {
	if len(body) != 8 {
		return 0, fmt.Errorf("legacy ready-check start has %d bytes, want 8", len(body))
	}
	return binary.LittleEndian.Uint64(body), nil
}

func ParseLegacyReadyCheckResponse(body []byte) (uint64, bool, error) {
	if len(body) != 9 {
		return 0, false, fmt.Errorf("legacy ready-check response has %d bytes, want 9", len(body))
	}
	return binary.LittleEndian.Uint64(body), body[8] != 0, nil
}

func TranslateLegacyGroupNewLeader(body []byte, partyIndex byte) ([]byte, error) {
	r := movementReader{data: body}
	name, err := readLegacyCString(&r, 511)
	if err != nil {
		return nil, fmt.Errorf("read new leader name: %w", err)
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("new-leader packet has %d trailing bytes", r.remaining())
	}
	name = truncateWireString(name, 511)
	body = []byte{partyIndex}
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(name)), 9)
	return append(bits.flush(), name...), nil
}

func EncodeRolePollInform(partyIndex byte, initiator GUID128) []byte {
	body := []byte{partyIndex}
	return appendPackedGUID128(body, initiator.Low, initiator.High)
}

func EncodeRoleChangedInform(partyIndex byte, from, target GUID128, oldRole, newRole byte) []byte {
	body := []byte{partyIndex}
	body = appendPackedGUID128(body, from.Low, from.High)
	body = appendPackedGUID128(body, target.Low, target.High)
	return append(body, oldRole, newRole)
}

func TranslateLegacyMinimapPing(body []byte, resolve func(uint64) GUID128) ([]byte, error) {
	if len(body) != 16 {
		return nil, fmt.Errorf("legacy minimap-ping has %d bytes, want 16", len(body))
	}
	sender := resolve(binary.LittleEndian.Uint64(body))
	result := appendPackedGUID128(nil, sender.Low, sender.High)
	return append(result, body[8:]...), nil
}

func TranslateLegacyRandomRoll(body []byte, resolve func(uint64) GUID128) ([]byte, error) {
	if len(body) != 20 {
		return nil, fmt.Errorf("legacy random-roll has %d bytes, want 20", len(body))
	}
	roller := resolve(binary.LittleEndian.Uint64(body[12:]))
	result := appendPackedGUID128(nil, roller.Low, roller.High)
	account := ModernWowAccountGUIDForLegacy(binary.LittleEndian.Uint64(body[12:]))
	result = appendPackedGUID128(result, account.Low, account.High)
	return append(result, body[:12]...), nil
}

func ParseLegacyPartySnapshot(body []byte, selfGUID uint64, selfName string) (LegacyPartySnapshot, error) {
	var snapshot LegacyPartySnapshot
	r := movementReader{data: body}
	groupType, err := r.u8()
	if err != nil {
		return snapshot, fmt.Errorf("read group type: %w", err)
	}
	snapshot.GroupType = groupType
	snapshot.IsRaid = groupType&0x02 != 0
	ownSubgroup, err := r.u8()
	if err != nil {
		return snapshot, fmt.Errorf("read own subgroup: %w", err)
	}
	ownFlags, err := r.u8()
	if err != nil {
		return snapshot, fmt.Errorf("read own flags: %w", err)
	}
	ownRoles, err := r.u8()
	if err != nil {
		return snapshot, fmt.Errorf("read own roles: %w", err)
	}
	if groupType&0x08 != 0 {
		if _, err = r.u8(); err != nil {
			return snapshot, fmt.Errorf("read LFG status: %w", err)
		}
		if _, err = r.u32(); err != nil {
			return snapshot, fmt.Errorf("read LFG dungeon: %w", err)
		}
	}
	if snapshot.LegacyGUID, err = r.u64(); err != nil {
		return snapshot, fmt.Errorf("read party GUID: %w", err)
	}
	snapshot.GUID = modernPartyGUID(snapshot.LegacyGUID)
	if groupType&0x01 != 0 {
		snapshot.PartyIndex = 1
	}
	if _, err = r.u32(); err != nil {
		return snapshot, fmt.Errorf("read party counter: %w", err)
	}
	memberCount, err := r.u32()
	if err != nil {
		return snapshot, fmt.Errorf("read party member count: %w", err)
	}
	if memberCount > 40 {
		return snapshot, fmt.Errorf("party has %d other members", memberCount)
	}
	if memberCount == 0 {
		snapshot.Destroyed = true
		// AzerothCore may append the old leader GUID to its final zero-member
		// group list. Consume it so malformed suffixes are still rejected.
		if r.remaining() == 8 {
			if snapshot.Leader, err = r.u64(); err != nil {
				return snapshot, fmt.Errorf("read destroyed party leader: %w", err)
			}
		}
		if r.remaining() != 0 {
			return snapshot, fmt.Errorf("destroyed party has %d trailing bytes", r.remaining())
		}
		return snapshot, nil
	}
	snapshot.Members = append(snapshot.Members, LegacyPartyMember{GUID: selfGUID, Name: selfName, Subgroup: ownSubgroup, Flags: ownFlags, Roles: ownRoles})
	for index := uint32(0); index < memberCount; index++ {
		var member LegacyPartyMember
		if member.Name, err = readLegacyCString(&r, maximumLegacyPartyMemberName); err != nil {
			return snapshot, fmt.Errorf("read party member %d name: %w", index, err)
		}
		if member.GUID, err = r.u64(); err != nil {
			return snapshot, fmt.Errorf("read party member %d GUID: %w", index, err)
		}
		if _, err = r.u8(); err != nil { // online status
			return snapshot, fmt.Errorf("read party member %d status: %w", index, err)
		}
		if member.Subgroup, err = r.u8(); err != nil {
			return snapshot, fmt.Errorf("read party member %d subgroup: %w", index, err)
		}
		if member.Flags, err = r.u8(); err != nil {
			return snapshot, fmt.Errorf("read party member %d flags: %w", index, err)
		}
		if member.Roles, err = r.u8(); err != nil {
			return snapshot, fmt.Errorf("read party member %d roles: %w", index, err)
		}
		snapshot.Members = append(snapshot.Members, member)
	}
	if snapshot.Leader, err = r.u64(); err != nil {
		return snapshot, fmt.Errorf("read party leader: %w", err)
	}
	if snapshot.LootMethod, err = r.u8(); err != nil {
		return snapshot, fmt.Errorf("read party loot method: %w", err)
	}
	if snapshot.LootMaster, err = r.u64(); err != nil {
		return snapshot, fmt.Errorf("read party loot master: %w", err)
	}
	if snapshot.LootThreshold, err = r.u8(); err != nil {
		return snapshot, fmt.Errorf("read party loot threshold: %w", err)
	}
	// Dungeon, raid, and legacy-raid difficulty bytes are cached by the
	// PartyUpdate translator; the snapshot only needs to validate them.
	if _, err = r.take(3); err != nil {
		return snapshot, fmt.Errorf("read party difficulties: %w", err)
	}
	if r.remaining() != 0 {
		return snapshot, fmt.Errorf("party snapshot has %d trailing bytes", r.remaining())
	}
	return snapshot, nil
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}
