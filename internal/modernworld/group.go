package modernworld

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
)

const (
	CMSGRequestPartyJoinUpdates = uint16(0x35F8)
	CMSGPartyInvite             = uint16(0x3604)
	CMSGPartyInviteResponse     = uint16(0x3606)
	CMSGLeaveGroup              = uint16(0x364C)
	CMSGUpdateRaidTarget        = uint16(0x3653)
	CMSGRequestPartyMemberStats = uint16(0x3656)
	SMSGPartyInvite             = uint16(0x25BD)
	SMSGRaidTargetUpdateAll     = uint16(0x262E)
	SMSGRaidTargetUpdateSingle  = uint16(0x262F)
	SMSGPartyMemberPartialState = uint16(0x2758)
	SMSGPartyMemberFullState    = uint16(0x2759)
	SMSGPartyUpdate             = uint16(0x25F4)
	SMSGGroupDecline            = uint16(0x2791)
	SMSGGroupUninvite           = uint16(0x2793)
	SMSGGroupDestroyed          = uint16(0x2794)
	SMSGPartyCommandResult      = uint16(0x2796)
)

type PartyInviteRequest struct {
	PartyIndex          byte
	VirtualRealmAddress uint32
	TargetGUID          GUID128
	TargetName          string
	TargetRealm         string
}

type PartyInviteResponse struct {
	Accept       bool
	PartyIndex   byte
	HasParty     bool
	RolesDesired byte
	HasRoles     bool
}

type PartyMemberStatsRequest struct {
	PartyIndex byte
	Target     GUID128
}

type RaidTargetRequest struct {
	PartyIndex byte
	Target     GUID128
	Symbol     byte
}

type LegacyRaidTarget struct {
	Symbol byte
	Target uint64
}

type RaidTarget struct {
	Symbol byte
	Target GUID128
}

type LegacyRaidTargetUpdate struct {
	All       bool
	ChangedBy uint64
	Symbol    byte
	Target    uint64
	Targets   []LegacyRaidTarget
}

func ParsePartyJoinUpdates(body []byte) error {
	// This is a modern party-UI subscription probe with no WotLK equivalent.
	// Build 54261 sends its two one-byte selectors when entering the world.
	if len(body) != 2 {
		return fmt.Errorf("party-join-updates has %d bytes, want 2", len(body))
	}
	return nil
}

func ParsePartyMemberStatsRequest(body []byte) (PartyMemberStatsRequest, error) {
	var request PartyMemberStatsRequest
	r := movementReader{data: body}
	var err error
	// 3.4.3 writes the party selector as a single optional bit before the
	// packed GUID.  The no-party form is byte-for-byte compatible with the
	// older leading-index layout, so only the set-bit form needs special
	// handling.  Build 54261 sends 0x80 for a selector that is present.
	modernSelector := len(body) > 0 && body[0]&0x80 != 0
	if modernSelector {
		if _, err = r.bit(); err != nil {
			return request, fmt.Errorf("read party-member-stats party flag: %w", err)
		}
		r.align()
	} else if request.PartyIndex, err = r.u8(); err != nil {
		return request, fmt.Errorf("read party-member-stats party index: %w", err)
	}
	if request.Target, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read party-member-stats target: %w", err)
	}
	if modernSelector {
		if request.PartyIndex, err = r.u8(); err != nil {
			return request, fmt.Errorf("read party-member-stats party index: %w", err)
		}
	}
	// Build 54261 appends a one-byte flag after the target GUID on some
	// requests (login-time refreshes). legacy proxy reads only the index and the
	// GUID and ignores whatever follows, so accept the trailing bytes here too
	// instead of dropping the whole refresh and starving the raid frame.
	if r.remaining() != 0 {
		if _, err = r.take(r.remaining()); err != nil {
			return request, fmt.Errorf("read party-member-stats trailing bytes: %w", err)
		}
	}
	return request, nil
}

func EncodeLegacyPartyMemberStatsRequest(target uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, target)
}

func ParseRaidTargetRequest(body []byte) (RaidTargetRequest, error) {
	var request RaidTargetRequest
	r := movementReader{data: body}
	var err error
	modernSelector := len(body) > 0 && body[0]&0x80 != 0
	if modernSelector {
		if _, err = r.bit(); err != nil {
			return request, fmt.Errorf("read raid-target party flag: %w", err)
		}
		r.align()
	} else if request.PartyIndex, err = r.u8(); err != nil {
		return request, fmt.Errorf("read raid-target party index: %w", err)
	}
	if request.Target, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read raid-target target: %w", err)
	}
	if request.Symbol, err = r.u8(); err != nil {
		return request, fmt.Errorf("read raid-target symbol: %w", err)
	}
	if modernSelector {
		if request.PartyIndex, err = r.u8(); err != nil {
			return request, fmt.Errorf("read raid-target party index: %w", err)
		}
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("raid-target request has %d trailing bytes", r.remaining())
	}
	if request.Symbol > 7 && request.Symbol != 0xff {
		return request, fmt.Errorf("raid-target symbol %d is invalid", request.Symbol)
	}
	return request, nil
}

func EncodeLegacyRaidTargetRequest(request RaidTargetRequest, target uint64) []byte {
	body := []byte{request.Symbol}
	return binary.LittleEndian.AppendUint64(body, target)
}

func ParseLegacyRaidTargetUpdate(body []byte) (LegacyRaidTargetUpdate, error) {
	var update LegacyRaidTargetUpdate
	r := movementReader{data: body}
	all, err := r.u8()
	if err != nil {
		return update, fmt.Errorf("read raid-target update kind: %w", err)
	}
	update.All = all != 0
	if update.All {
		if r.remaining()%9 != 0 {
			return update, fmt.Errorf("raid-target list has %d malformed bytes", r.remaining())
		}
		count := r.remaining() / 9
		if count > 8 {
			return update, fmt.Errorf("raid-target list has %d entries, maximum is 8", count)
		}
		update.Targets = make([]LegacyRaidTarget, count)
		for index := range update.Targets {
			if update.Targets[index].Symbol, err = r.u8(); err != nil {
				return update, fmt.Errorf("read raid-target %d symbol: %w", index, err)
			}
			if update.Targets[index].Target, err = r.u64(); err != nil {
				return update, fmt.Errorf("read raid-target %d GUID: %w", index, err)
			}
		}
		return update, nil
	}
	if update.ChangedBy, err = r.u64(); err != nil {
		return update, fmt.Errorf("read raid-target changer: %w", err)
	}
	if update.Symbol, err = r.u8(); err != nil {
		return update, fmt.Errorf("read raid-target symbol: %w", err)
	}
	if update.Target, err = r.u64(); err != nil {
		return update, fmt.Errorf("read raid-target GUID: %w", err)
	}
	if r.remaining() != 0 {
		return update, fmt.Errorf("raid-target update has %d trailing bytes", r.remaining())
	}
	return update, nil
}

func EncodeRaidTargetUpdateSingle(partyIndex, symbol byte, target, changedBy GUID128) []byte {
	body := []byte{partyIndex, symbol}
	body = appendPackedGUID128(body, target.Low, target.High)
	return appendPackedGUID128(body, changedBy.Low, changedBy.High)
}

func EncodeRaidTargetUpdateAll(partyIndex byte, targets []RaidTarget) []byte {
	body := []byte{partyIndex}
	body = binary.LittleEndian.AppendUint32(body, uint32(len(targets)))
	for _, target := range targets {
		body = appendPackedGUID128(body, target.Target.Low, target.Target.High)
		body = append(body, target.Symbol)
	}
	return body
}

type LegacyPartyInvite struct {
	CanAccept     bool
	InviterName   string
	ProposedRoles uint32
	LFGSlots      []int32
	CompletedMask int32
}

type PartyPlayer struct {
	GUID         GUID128
	Name         string
	Status       byte
	Subgroup     byte
	Flags        byte
	Roles        byte
	ClassID      byte
	FactionGroup byte
}

func ParsePartyInvite(body []byte) (PartyInviteRequest, error) {
	var request PartyInviteRequest
	r := movementReader{data: body}
	var err error
	if request.PartyIndex, err = r.u8(); err != nil {
		return request, fmt.Errorf("read party index: %w", err)
	}
	nameLength, err := r.bits(9)
	if err != nil {
		return request, fmt.Errorf("read invite name length: %w", err)
	}
	realmLength, err := r.bits(9)
	if err != nil {
		return request, fmt.Errorf("read invite realm length: %w", err)
	}
	if request.VirtualRealmAddress, err = r.u32(); err != nil {
		return request, fmt.Errorf("read invite virtual realm: %w", err)
	}
	if request.TargetGUID, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read invite target GUID: %w", err)
	}
	if request.TargetName, err = r.stringN(int(nameLength)); err != nil {
		return request, fmt.Errorf("read invite target name: %w", err)
	}
	if request.TargetRealm, err = r.stringN(int(realmLength)); err != nil {
		return request, fmt.Errorf("read invite target realm: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("party invite has %d trailing bytes", r.remaining())
	}
	if strings.TrimSpace(request.TargetName) == "" {
		return request, fmt.Errorf("party invite target name is empty")
	}
	return request, nil
}

func EncodeLegacyPartyInvite(name string) []byte {
	body := append([]byte(nil), name...)
	body = append(body, 0)
	return binary.LittleEndian.AppendUint32(body, 0) // proposed roles
}

func ParsePartyInviteResponse(body []byte) (PartyInviteResponse, error) {
	var response PartyInviteResponse
	r := movementReader{data: body}
	var err error
	if response.HasParty, err = r.bit(); err != nil {
		return response, fmt.Errorf("read invite-response party flag: %w", err)
	}
	if response.Accept, err = r.bit(); err != nil {
		return response, fmt.Errorf("read invite-response accept flag: %w", err)
	}
	if response.HasRoles, err = r.bit(); err != nil {
		return response, fmt.Errorf("read invite-response roles flag: %w", err)
	}
	if response.HasParty {
		if response.PartyIndex, err = r.u8(); err != nil {
			return response, fmt.Errorf("read invite-response party index: %w", err)
		}
	}
	if response.HasRoles {
		if response.RolesDesired, err = r.u8(); err != nil {
			return response, fmt.Errorf("read invite-response roles: %w", err)
		}
	}
	r.align()
	if r.remaining() != 0 {
		return response, fmt.Errorf("party invite response has %d trailing bytes", r.remaining())
	}
	return response, nil
}

func EncodeLegacyPartyInviteResponse(response PartyInviteResponse) []byte {
	if !response.Accept {
		return nil
	}
	return binary.LittleEndian.AppendUint32(nil, 0)
}

func ParseLegacyPartyInvite(body []byte) (LegacyPartyInvite, error) {
	var invite LegacyPartyInvite
	r := movementReader{data: body}
	canAccept, err := r.u8()
	if err != nil {
		return invite, fmt.Errorf("read legacy invite accept flag: %w", err)
	}
	invite.CanAccept = canAccept != 0
	invite.InviterName, err = readLegacyCString(&r, 63)
	if err != nil {
		return invite, fmt.Errorf("read legacy inviter name: %w", err)
	}
	if invite.ProposedRoles, err = r.u32(); err != nil {
		return invite, fmt.Errorf("read legacy proposed roles: %w", err)
	}
	count, err := r.u8()
	if err != nil {
		return invite, fmt.Errorf("read legacy LFG slot count: %w", err)
	}
	if count > 64 {
		return invite, fmt.Errorf("legacy invite has %d LFG slots", count)
	}
	invite.LFGSlots = make([]int32, count)
	for index := range invite.LFGSlots {
		invite.LFGSlots[index], err = r.i32()
		if err != nil {
			return invite, fmt.Errorf("read legacy LFG slot %d: %w", index, err)
		}
	}
	invite.CompletedMask, err = r.i32()
	if err != nil {
		return invite, fmt.Errorf("read legacy LFG completed mask: %w", err)
	}
	if r.remaining() != 0 {
		return invite, fmt.Errorf("legacy party invite has %d trailing bytes", r.remaining())
	}
	return invite, nil
}

func EncodePartyInvite(invite LegacyPartyInvite, realmAddress uint32, realmName string) []byte {
	name := truncateWireString(invite.InviterName, 63)
	normalized := strings.ReplaceAll(realmName, " ", "")
	realmName = truncateWireString(realmName, 255)
	normalized = truncateWireString(normalized, 255)
	bits := newBitWriter(nil)
	bits.writeBit(invite.CanAccept)
	for range 5 {
		bits.writeBit(false)
	}
	bits.writeBits(uint32(len(name)), 6)
	body := bits.flush()
	body = binary.LittleEndian.AppendUint32(body, realmAddress)
	realmBits := newBitWriter(body)
	realmBits.writeBit(true)
	realmBits.writeBit(false)
	realmBits.writeBits(uint32(len(realmName)), 8)
	realmBits.writeBits(uint32(len(normalized)), 8)
	body = realmBits.flush()
	body = append(body, realmName...)
	body = append(body, normalized...)
	inviter := modernPlayerGUIDForName(name)
	body = appendPackedGUID128(body, inviter.Low, inviter.High)
	body = appendPackedGUID128(body, 0, 0)
	body = binary.LittleEndian.AppendUint16(body, 4904)
	body = append(body, byte(invite.ProposedRoles)) // build 54261 uses uint8 for a WotLK-backed invite
	body = binary.LittleEndian.AppendUint32(body, uint32(len(invite.LFGSlots)))
	body = binary.LittleEndian.AppendUint32(body, uint32(invite.CompletedMask))
	body = append(body, name...)
	for _, slot := range invite.LFGSlots {
		body = binary.LittleEndian.AppendUint32(body, uint32(slot))
	}
	return body
}

func TranslateLegacyGroupList(body []byte, self PartyPlayer, selfLegacy uint64, sequence int32, resolve func(uint64) PartyPlayer) ([]byte, []uint64, error) {
	return TranslateLegacyGroupListWithOrder(body, self, selfLegacy, sequence, resolve, nil)
}

// TranslateLegacyGroupListWithOrder lets the proxy share a stable roster order
// across recipients. chooseOrder runs only after the complete packet validates;
// its members argument contains all members sorted by legacy GUID.
func TranslateLegacyGroupListWithOrder(body []byte, self PartyPlayer, selfLegacy uint64, sequence int32, resolve func(uint64) PartyPlayer, chooseOrder func(uint64, uint64, []uint64) []uint64) ([]byte, []uint64, error) {
	r := movementReader{data: body}
	groupType, err := r.u8()
	if err != nil {
		return nil, nil, fmt.Errorf("read group type: %w", err)
	}
	ownSubgroup, err := r.u8()
	if err != nil {
		return nil, nil, fmt.Errorf("read own subgroup: %w", err)
	}
	ownFlags, err := r.u8()
	if err != nil {
		return nil, nil, fmt.Errorf("read own group flags: %w", err)
	}
	ownRoles, err := r.u8()
	if err != nil {
		return nil, nil, fmt.Errorf("read own LFG roles: %w", err)
	}
	isBattleground := groupType&0x01 != 0
	isRaid := groupType&0x02 != 0
	isLFG := groupType&0x08 != 0
	if isLFG {
		if _, err = r.u8(); err != nil {
			return nil, nil, fmt.Errorf("read LFG dungeon status: %w", err)
		}
		if _, err = r.u32(); err != nil {
			return nil, nil, fmt.Errorf("read LFG dungeon ID: %w", err)
		}
	}
	legacyPartyGUID, err := r.u64()
	if err != nil {
		return nil, nil, fmt.Errorf("read party GUID: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return nil, nil, fmt.Errorf("read group counter: %w", err)
	}
	membersCount, err := r.u32()
	if err != nil {
		return nil, nil, fmt.Errorf("read group member count: %w", err)
	}
	if membersCount > 40 {
		return nil, nil, fmt.Errorf("group has %d other members", membersCount)
	}
	partyIndex := byte(0)
	if isBattleground {
		partyIndex = 1
	}
	if membersCount == 0 {
		// AzerothCore still appends the leader GUID when the last member leaves.
		// Treat that zero-member roster as destroyed after consuming the suffix.
		if r.remaining() == 8 {
			if _, err = r.u64(); err != nil {
				return nil, nil, fmt.Errorf("read destroyed group leader: %w", err)
			}
		}
		if r.remaining() != 0 {
			return nil, nil, fmt.Errorf("destroyed group list has %d unexpected bytes", r.remaining())
		}
		return encodePartyUpdate(0x10, partyIndex, 0, -1, GUID128{}, sequence, GUID128{}, nil, nil, nil), nil, nil
	}
	self.Subgroup = ownSubgroup
	self.Flags = ownFlags
	self.Roles = ownRoles
	self.Status = 1
	self.FactionGroup = 0
	type parsedMember struct {
		legacy uint64
		player PartyPlayer
	}
	others := make([]parsedMember, 0, membersCount)
	allAssist := true
	for index := uint32(0); index < membersCount; index++ {
		var player PartyPlayer
		player.Name, err = readLegacyCString(&r, 63)
		if err != nil {
			return nil, nil, fmt.Errorf("read group member %d name: %w", index, err)
		}
		legacyGUID, readErr := r.u64()
		if readErr != nil {
			return nil, nil, fmt.Errorf("read group member %d GUID: %w", index, readErr)
		}
		resolved := resolve(legacyGUID)
		player.GUID = resolved.GUID
		player.ClassID = resolved.ClassID
		// legacy proxy leaves PartyPlayerInfo::FactionGroup at its zero value for
		// legacy group-list translations. Match that wire format exactly so the
		// client's roster identity does not diverge from the reference proxy.
		player.FactionGroup = 0
		if player.Status, err = r.u8(); err != nil {
			return nil, nil, fmt.Errorf("read group member %d status: %w", index, err)
		}
		if player.Subgroup, err = r.u8(); err != nil {
			return nil, nil, fmt.Errorf("read group member %d subgroup: %w", index, err)
		}
		// The 54261 client renders the roster subgroup field N as raid group
		// N+1, so a WotLK 0-indexed subgroup must be forwarded unchanged; a +1
		// here pushes the member into the wrong raid group. HermesProxy also
		// writes the raw WotLK value.
		if player.Flags, err = r.u8(); err != nil {
			return nil, nil, fmt.Errorf("read group member %d flags: %w", index, err)
		}
		if player.Roles, err = r.u8(); err != nil {
			return nil, nil, fmt.Errorf("read group member %d roles: %w", index, err)
		}
		if player.Flags&0x01 == 0 {
			allAssist = false
		}
		others = append(others, parsedMember{legacy: legacyGUID, player: player})
	}
	legacyLeader, err := r.u64()
	if err != nil {
		return nil, nil, fmt.Errorf("read group leader: %w", err)
	}
	lootMethod, err := r.u8()
	if err != nil {
		return nil, nil, fmt.Errorf("read group loot method: %w", err)
	}
	legacyLootMaster, err := r.u64()
	if err != nil {
		return nil, nil, fmt.Errorf("read group loot master: %w", err)
	}
	lootThreshold, err := r.u8()
	if err != nil {
		return nil, nil, fmt.Errorf("read group loot threshold: %w", err)
	}
	dungeonDifficulty, err := r.u8()
	if err != nil {
		return nil, nil, fmt.Errorf("read group dungeon difficulty: %w", err)
	}
	raidDifficulty, err := r.u8()
	if err != nil {
		return nil, nil, fmt.Errorf("read group raid difficulty: %w", err)
	}
	dynamicRaidDifficulty, err := r.u8()
	if err != nil {
		return nil, nil, fmt.Errorf("read group dynamic raid difficulty: %w", err)
	}
	if r.remaining() != 0 {
		return nil, nil, fmt.Errorf("group list has %d trailing bytes", r.remaining())
	}
	byLegacy := make(map[uint64]PartyPlayer, len(others)+1)
	byLegacy[selfLegacy] = self
	for _, member := range others {
		if _, exists := byLegacy[member.legacy]; !exists {
			byLegacy[member.legacy] = member.player
		}
	}
	// Every recipient reconstructs the same complete roster independently.
	// Keep subgroup separate so moving groups cannot change raid unit indices.
	order := make([]uint64, 0, len(byLegacy))
	for guid := range byLegacy {
		order = append(order, guid)
	}
	slices.Sort(order)
	if chooseOrder != nil {
		preferred := chooseOrder(legacyPartyGUID, legacyLeader, slices.Clone(order))
		ordered := make([]uint64, 0, len(order))
		included := make(map[uint64]bool, len(order))
		for _, guid := range append(preferred, order...) {
			if _, present := byLegacy[guid]; present && !included[guid] {
				ordered = append(ordered, guid)
				included[guid] = true
			}
		}
		order = ordered
	}
	players := make([]PartyPlayer, 0, len(order))
	seen := make(map[GUID128]struct{}, len(order))
	myIndex := int32(-1)
	for _, legacyGUID := range order {
		player, ok := byLegacy[legacyGUID]
		if !ok {
			continue
		}
		if legacyGUID == selfLegacy {
			player = self
		}
		if _, duplicate := seen[player.GUID]; duplicate {
			continue
		}
		seen[player.GUID] = struct{}{}
		if legacyGUID == selfLegacy {
			myIndex = int32(len(players))
		}
		players = append(players, player)
	}
	if myIndex < 0 {
		myIndex = 0
	}
	// 0x10 is PartyUpdate's destroyed flag. legacy proxy only sets it for an
	// empty roster; a live roster starts at zero and adds its group-type flags.
	flags := uint16(0)
	if partyIndex != 0 {
		flags |= 0x01
	}
	if isRaid {
		flags |= 0x02
	}
	if isLFG {
		flags |= 0x08
	}
	if allAssist {
		flags |= 0x40
	}
	partyType := byte(1)
	if partyIndex != 0 {
		partyType = 2
	}
	partyGUID := modernPartyGUID(legacyPartyGUID)
	leader := resolve(legacyLeader).GUID
	lootMaster := resolve(legacyLootMaster).GUID
	loot := &partyLootSettings{method: lootMethod, master: lootMaster, threshold: lootThreshold}
	difficulties := &partyDifficultySettings{
		dungeon: modernDungeonDifficulty(dungeonDifficulty),
		raid:    modernRaidDifficulty(raidDifficulty),
		legacy:  modernRaidDifficulty(dynamicRaidDifficulty),
	}
	// MyIndex identifies self in the final shared PlayerList.
	return encodePartyUpdate(flags, partyIndex, partyType, myIndex, partyGUID, sequence, leader, players, loot, difficulties), order, nil
}

func modernDungeonDifficulty(legacy byte) uint32 {
	if legacy == 0 {
		return 1
	}
	return 2
}

func modernRaidDifficulty(legacy byte) uint32 {
	switch legacy {
	case 0:
		return 3 // 10-player normal
	case 1:
		return 4 // 25-player normal
	case 2:
		return 5 // 10-player heroic
	case 3:
		return 6 // 25-player heroic
	default:
		return 0
	}
}

type partyLootSettings struct {
	method    byte
	master    GUID128
	threshold byte
}

type partyDifficultySettings struct {
	dungeon uint32
	raid    uint32
	legacy  uint32
}

func encodePartyUpdate(flags uint16, partyIndex, partyType byte, myIndex int32, partyGUID GUID128, sequence int32, leader GUID128, players []PartyPlayer, loot *partyLootSettings, difficulty *partyDifficultySettings) []byte {
	body := binary.LittleEndian.AppendUint16(nil, flags)
	body = append(body, partyIndex, partyType)
	body = binary.LittleEndian.AppendUint32(body, uint32(myIndex))
	body = appendPackedGUID128(body, partyGUID.Low, partyGUID.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(sequence))
	body = appendPackedGUID128(body, leader.Low, leader.High)
	body = append(body, 0) // legacy proxy leaves leader faction group unset.
	// Build 54261 reads the player count immediately after the leader faction.
	// RestrictPingsTo was added by a later Classic client build; writing it here
	// shifts the roster and makes the 3.4.3 client discard every party member.
	body = binary.LittleEndian.AppendUint32(body, uint32(len(players)))
	optionBits := newBitWriter(body)
	optionBits.writeBit(false)
	optionBits.writeBit(loot != nil)
	optionBits.writeBit(difficulty != nil)
	body = optionBits.flush()
	for _, player := range players {
		name := truncateWireString(player.Name, 63)
		playerBits := newBitWriter(body)
		playerBits.writeBits(uint32(len(name)), 6)
		playerBits.writeBits(1, 6) // empty VoiceStateID is encoded as length+1
		// legacy proxy leaves all three per-player status bits unset. The client
		// still renders the member from the GUID and name; preserve that exact
		// shape for roster-dependent actions such as selecting a loot master.
		playerBits.writeBit(false)
		playerBits.writeBit(false)
		playerBits.writeBit(false)
		body = playerBits.flush()
		body = appendPackedGUID128(body, player.GUID.Low, player.GUID.High)
		body = append(body, player.Subgroup, player.Flags, player.Roles, player.ClassID, player.FactionGroup)
		body = append(body, name...)
	}
	if loot != nil {
		body = append(body, loot.method)
		body = appendPackedGUID128(body, loot.master.Low, loot.master.High)
		body = append(body, loot.threshold)
	}
	if difficulty != nil {
		body = binary.LittleEndian.AppendUint32(body, difficulty.dungeon)
		body = binary.LittleEndian.AppendUint32(body, difficulty.raid)
		body = binary.LittleEndian.AppendUint32(body, difficulty.legacy)
	}
	return body
}

func EncodeDestroyedPartyUpdate(sequence int32) []byte {
	return encodePartyUpdate(0x10, 0, 0, -1, GUID128{}, sequence, GUID128{}, nil, nil, nil)
}

func modernPartyGUID(legacy uint64) GUID128 {
	if legacy == 0 {
		return GUID128{}
	}
	// Legacy HighGuid::Group maps to the modern global RaidGroup GUID type.
	// It is not realm-specific; adding a realm bit makes PartyUpdate and the
	// ready/role packets refer to different logical groups in the client cache.
	return GUID128{Low: uint64(uint32(legacy)), High: uint64(33) << 58}
}

func FactionGroupForRace(race byte) byte {
	switch race {
	case 1, 3, 4, 7, 11:
		return 2 // Alliance
	case 2, 5, 6, 8, 10:
		return 1 // Horde
	default:
		return 0
	}
}

func LegacyPartyPlayerClassAndFaction(fields map[int]uint32) (byte, byte) {
	bytes0 := fields[legacyUnitBytes0]
	return byte(bytes0 >> 8), FactionGroupForRace(byte(bytes0))
}

func TranslateLegacyGroupDecline(body []byte) ([]byte, error) {
	r := movementReader{data: body}
	name, err := readLegacyCString(&r, 511)
	if err != nil {
		return nil, err
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("group decline has %d trailing bytes", r.remaining())
	}
	name = truncateWireString(name, 511)
	bits := newBitWriter(nil)
	bits.writeBits(uint32(len(name)), 9)
	return append(bits.flush(), name...), nil
}

func TranslateLegacyPartyCommandResult(body []byte) ([]byte, error) {
	r := movementReader{data: body}
	command, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read party command: %w", err)
	}
	name, err := readLegacyCString(&r, 511)
	if err != nil {
		return nil, fmt.Errorf("read party command name: %w", err)
	}
	result, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read party command result: %w", err)
	}
	resultData, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read party command result data: %w", err)
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("party command result has %d trailing bytes", r.remaining())
	}
	name = truncateWireString(name, 511)
	bits := newBitWriter(nil)
	bits.writeBits(uint32(len(name)), 9)
	bits.writeBits(command&0x0f, 4)
	bits.writeBits(result&0x3f, 6)
	out := bits.flush()
	out = binary.LittleEndian.AppendUint32(out, resultData)
	out = appendPackedGUID128(out, 0, 0)
	return append(out, name...), nil
}
