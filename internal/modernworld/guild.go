package modernworld

// Guild wire formats: HermesProxy-WOTLK build 54261, with AzerothCore
// GuildPackets.cpp for the 3.3.5a side. See docs/validation/2026-09-14-guild.md.
import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

const (
	CMSGGuildPromoteMember              uint16 = 0x305D
	CMSGGuildDemoteMember               uint16 = 0x305E
	CMSGGuildDeclineInvitation          uint16 = 0x3060
	CMSGGuildAutoDeclineInvitation      uint16 = 0x3061
	CMSGGuildLeave                      uint16 = 0x3062
	CMSGGuildOfficerRemoveMember        uint16 = 0x3063
	CMSGGuildAddRank                    uint16 = 0x3064
	CMSGGuildDeleteRank                 uint16 = 0x3065
	CMSGGuildSetRankPermissions         uint16 = 0x3067
	CMSGGuildDelete                     uint16 = 0x3068
	CMSGGuildGetRanks                   uint16 = 0x306D
	CMSGGuildSetMemberNote              uint16 = 0x3072
	CMSGGuildGetRoster                  uint16 = 0x3073
	CMSGGuildUpdateMOTDText             uint16 = 0x3074
	CMSGGuildUpdateInfoText             uint16 = 0x3075
	CMSGGuildBankLogQuery               uint16 = 0x3082
	CMSGGuildPermissionsQuery           uint16 = 0x3084
	CMSGGuildEventLogQuery              uint16 = 0x3085
	CMSGGuildBankSetTabText             uint16 = 0x3086
	CMSGGuildBankTextQuery              uint16 = 0x3087
	CMSGSaveGuildEmblem                 uint16 = 0x32A8
	CMSGTabardVendorActivate            uint16 = 0x32A9
	CMSGGuildBankActivate               uint16 = 0x34B5
	CMSGAutoGuildBankItem               uint16 = 0x34B6
	CMSGStoreGuildBankItem              uint16 = 0x34B7
	CMSGSwapItemWithGuildBankItem       uint16 = 0x34B8
	CMSGSwapGuildBankItem               uint16 = 0x34B9
	CMSGMoveGuildBankItem               uint16 = 0x34BA
	CMSGMergeItemWithGuildBankItem      uint16 = 0x34BB
	CMSGSplitItemToGuildBank            uint16 = 0x34BC
	CMSGMergeGuildBankItemWithItem      uint16 = 0x34BD
	CMSGSplitGuildBankItemToInventory   uint16 = 0x34BE
	CMSGAutoStoreGuildBankItem          uint16 = 0x34BF
	CMSGMergeGuildBankItem              uint16 = 0x34C0
	CMSGSplitGuildBankItem              uint16 = 0x34C1
	CMSGGuildBankQueryTab               uint16 = 0x34C2
	CMSGGuildBankBuyTab                 uint16 = 0x34C3
	CMSGGuildBankUpdateTab              uint16 = 0x34C4
	CMSGGuildBankDepositMoney           uint16 = 0x34C5
	CMSGGuildBankWithdrawMoney          uint16 = 0x34C6
	CMSGDeclineGuildInvites             uint16 = 0x351D
	CMSGAcceptGuildInvite               uint16 = 0x35FE
	CMSGGuildInviteByName               uint16 = 0x3608
	CMSGQueryGuildInfo                  uint16 = 0x368B
	CMSGGuildSetGuildMaster             uint16 = 0x36D0
	SMSGGuildSendRankChange             uint16 = 0x29B9
	SMSGGuildCommandResult              uint16 = 0x29BA
	SMSGGuildRoster                     uint16 = 0x29BB
	SMSGGuildRanks                      uint16 = 0x29C9
	SMSGGuildInvite                     uint16 = 0x29CB
	SMSGGuildBankQueryResults           uint16 = 0x29DF
	SMSGGuildBankLogQueryResults        uint16 = 0x29E0
	SMSGGuildBankRemainingWithdrawMoney uint16 = 0x29E1
	SMSGGuildPermissionsQueryResults    uint16 = 0x29E2
	SMSGGuildEventLogQueryResults       uint16 = 0x29E3
	SMSGGuildBankTextQueryResult        uint16 = 0x29E4
	SMSGQueryGuildInfoResponse          uint16 = 0x29E6
	SMSGGuildInviteDeclined             uint16 = 0x29E9
	SMSGPlayerSaveGuildEmblem           uint16 = 0x29F9
)

// guildReader retains the first error; callers publish no packets or state
// until done succeeds. Counts are bounded before allocating or looping.
type guildReader struct {
	r   movementReader
	err error
}

func guildRead(body []byte) *guildReader { return &guildReader{r: movementReader{data: body}} }
func guildValue[T any](r *guildReader, v T, err error) T {
	if r.err == nil {
		r.err = err
	}
	return v
}
func (r *guildReader) u8() byte          { v, e := r.r.u8(); return guildValue(r, v, e) }
func (r *guildReader) u32() uint32       { v, e := r.r.u32(); return guildValue(r, v, e) }
func (r *guildReader) u64() uint64       { v, e := r.r.u64(); return guildValue(r, v, e) }
func (r *guildReader) bits(n int) uint32 { v, e := r.r.bits(n); return guildValue(r, v, e) }
func (r *guildReader) guid() GUID128     { v, e := r.r.guid128(); return guildValue(r, v, e) }
func (r *guildReader) str(n uint32) string {
	v, e := r.r.stringN(int(n))
	v = guildValue(r, v, e)
	if strings.ContainsRune(v, 0) {
		r.fail("embedded NUL")
	}
	return v
}
func (r *guildReader) cstr(max int) string {
	v, e := readLegacyCString(&r.r, max)
	return guildValue(r, v, e)
}
func (r *guildReader) fail(s string) {
	if r.err == nil {
		r.err = fmt.Errorf("guild: %s", s)
	}
}
func (r *guildReader) count(n uint32, max uint32) int {
	if n > max {
		r.fail("count exceeds protocol limit")
		return 0
	}
	return int(n)
}
func (r *guildReader) done() error {
	r.r.align()
	if r.r.remaining() != 0 {
		r.fail("trailing bytes")
	}
	return r.err
}
func guildCString(s string) []byte { return append([]byte(s), 0) }
func guildU32(v uint32) []byte     { return binary.LittleEndian.AppendUint32(nil, v) }
func guildStrings(dst []byte, widths []int, ss ...string) []byte {
	w := newBitWriter(dst)
	for i, s := range ss {
		w.writeBits(uint32(len(s)), widths[i])
	}
	dst = w.flush()
	for _, s := range ss {
		dst = append(dst, s...)
	}
	return dst
}
func GuildGUID(id uint32) GUID128 {
	if id == 0 {
		return GUID128{}
	}
	return GUID128{Low: uint64(id), High: uint64(28)<<58 | uint64(1)<<42}
}
func guildAppendGUID(dst []byte, g GUID128) []byte { return appendPackedGUID128(dst, g.Low, g.High) }

type GuildRequest struct {
	Opcode       uint16
	Body         []byte
	Target       GUID128
	NeedsName    bool
	QueryGuildID uint32
	BlockInvites *bool
}

func IsGuildClientOpcode(op uint16) bool {
	if op == CMSGDeclineGuildInvites {
		return true
	}
	if op == CMSGTabardVendorActivate {
		return true
	}
	switch op {
	case CMSGGuildPromoteMember, CMSGGuildDemoteMember, CMSGGuildDeclineInvitation, CMSGGuildAutoDeclineInvitation, CMSGGuildLeave, CMSGGuildOfficerRemoveMember, CMSGGuildAddRank, CMSGGuildDeleteRank, CMSGGuildSetRankPermissions, CMSGGuildDelete, CMSGGuildGetRanks, CMSGGuildSetMemberNote, CMSGGuildGetRoster, CMSGGuildUpdateMOTDText, CMSGGuildUpdateInfoText, CMSGGuildBankLogQuery, CMSGGuildBankRemainingWithdrawQuery, CMSGGuildPermissionsQuery, CMSGGuildEventLogQuery, CMSGGuildBankSetTabText, CMSGGuildBankTextQuery, CMSGSaveGuildEmblem, CMSGAcceptGuildInvite, CMSGGuildInviteByName, CMSGQueryGuildInfo, CMSGGuildSetGuildMaster:
		return true
	}
	return op >= CMSGGuildBankActivate && op <= CMSGGuildBankWithdrawMoney
}

// ParseGuildRequest converts scalar/string payloads; GUID resolution and name
// lookup remain session-local. All legacy authority checks stay on the server.
func ParseGuildRequest(op uint16, body []byte, resolve func(GUID128) uint64) (q GuildRequest, err error) {
	r := guildRead(body)
	bankGUID := func() { g := r.guid(); q.Body = binary.LittleEndian.AppendUint64(q.Body, resolve(g)) }
	tab := func(v uint32, log bool) byte {
		max := uint32(5)
		if log {
			max = 6
		}
		if v > max {
			r.fail("invalid bank tab")
		}
		return byte(v)
	}
	switch op {
	case CMSGDeclineGuildInvites:
		v := r.u8()
		if v > 1 {
			r.fail("invalid auto-decline flag")
		}
		blocked := v != 0
		q.BlockInvites = &blocked
	case CMSGAcceptGuildInvite:
		q.Opcode = 0x84
	case CMSGGuildDeclineInvitation, CMSGGuildAutoDeclineInvitation:
		q.Opcode = 0x85
	case CMSGGuildLeave:
		q.Opcode = 0x8D
	case CMSGGuildDelete:
		q.Opcode = 0x8F
	case CMSGGuildGetRoster:
		q.Opcode = 0x89
	case CMSGGuildGetRanks:
		r.guid()
		q.Opcode = 0x89
	case CMSGGuildPermissionsQuery:
		q.Opcode = 0x3FD
	case CMSGGuildBankRemainingWithdrawQuery:
		q.Opcode = 0x3FE
	case CMSGGuildEventLogQuery:
		q.Opcode = 0x3FF
	case CMSGGuildDeleteRank:
		r.count(r.u32(), 9)
		q.Opcode = 0x233
	case CMSGGuildPromoteMember, CMSGGuildDemoteMember, CMSGGuildOfficerRemoveMember:
		q.Opcode = map[uint16]uint16{CMSGGuildPromoteMember: 0x8B, CMSGGuildDemoteMember: 0x8C, CMSGGuildOfficerRemoveMember: 0x8E}[op]
		q.Target = r.guid()
		q.NeedsName = true
	case CMSGGuildSetMemberNote:
		q.Target = r.guid()
		q.NeedsName = true
		n := r.bits(8)
		q.Opcode = 0x235
		if r.bits(1) != 0 {
			q.Opcode = 0x234
		}
		q.Body = guildCString(r.str(n))
	case CMSGGuildUpdateMOTDText, CMSGGuildUpdateInfoText, CMSGGuildSetGuildMaster:
		width := 11
		q.Opcode = 0x91
		if op == CMSGGuildUpdateInfoText {
			q.Opcode = 0x2FC
		}
		if op == CMSGGuildSetGuildMaster {
			width = 9
			q.Opcode = 0x90
		}
		q.Body = guildCString(r.str(r.bits(width)))
	case CMSGGuildInviteByName:
		n := r.bits(9)
		arena := r.bits(1) != 0
		name := r.str(n)
		q.Opcode = 0x82
		if arena {
			q.Opcode = 0x34F
			q.Body = guildU32(r.u32())
		}
		q.Body = append(q.Body, guildCString(name)...)
	case CMSGGuildAddRank:
		n := r.bits(7)
		r.count(r.u32(), 9)
		q.Opcode = 0x232
		q.Body = guildCString(r.str(n))
	case CMSGGuildSetRankPermissions:
		rank := r.u32()
		r.count(rank, 9)
		r.count(r.u32(), 9)
		flags, money := r.u32(), r.u32()
		tabs := make([]uint32, 12)
		for i := range tabs {
			tabs[i] = r.u32()
		}
		r.u32()
		name := r.str(r.bits(7))
		q.Opcode = 0x231
		q.Body = guildU32(rank)
		q.Body = binary.LittleEndian.AppendUint32(q.Body, flags)
		q.Body = append(q.Body, guildCString(name)...)
		q.Body = binary.LittleEndian.AppendUint32(q.Body, money)
		for _, v := range tabs {
			q.Body = binary.LittleEndian.AppendUint32(q.Body, v)
		}
	case CMSGQueryGuildInfo:
		g := r.guid()
		r.guid()
		if g.Low > math.MaxUint32 {
			r.fail("guild ID overflow")
		}
		q.QueryGuildID = uint32(g.Low)
		q.Opcode = 0x54
		q.Body = guildU32(q.QueryGuildID)
	case CMSGSaveGuildEmblem:
		q.Opcode = 0x1F1
		bankGUID()
		for i := 0; i < 5; i++ {
			q.Body = binary.LittleEndian.AppendUint32(q.Body, r.u32())
		}
	case CMSGTabardVendorActivate:
		q.Opcode = 0x1F2
		bankGUID()
	case CMSGGuildBankActivate:
		q.Opcode = 0x3E6
		bankGUID()
		q.Body = append(q.Body, byte(r.bits(1)))
	case CMSGGuildBankQueryTab:
		q.Opcode = 0x3E7
		bankGUID()
		q.Body = append(q.Body, tab(uint32(r.u8()), false), byte(r.bits(1)))
	case CMSGGuildBankBuyTab:
		q.Opcode = 0x3EA
		bankGUID()
		q.Body = append(q.Body, tab(uint32(r.u8()), false))
	case CMSGGuildBankDepositMoney, CMSGGuildBankWithdrawMoney:
		q.Opcode = 0x3EC
		if op == CMSGGuildBankWithdrawMoney {
			q.Opcode = 0x3ED
		}
		bankGUID()
		money := r.u64()
		if money > math.MaxUint32 {
			r.fail("money exceeds WotLK uint32 limit")
		}
		q.Body = binary.LittleEndian.AppendUint32(q.Body, uint32(money))
	case CMSGGuildBankUpdateTab:
		q.Opcode = 0x3EB
		bankGUID()
		q.Body = append(q.Body, tab(uint32(r.u8()), false))
		n, i := r.bits(7), r.bits(9)
		q.Body = append(q.Body, guildCString(r.str(n))...)
		q.Body = append(q.Body, guildCString(r.str(i))...)
	case CMSGGuildBankLogQuery, CMSGGuildBankTextQuery, CMSGGuildBankSetTabText:
		q.Opcode = 0x3EE
		if op == CMSGGuildBankTextQuery {
			q.Opcode = 0x40A
		}
		if op == CMSGGuildBankSetTabText {
			q.Opcode = 0x40B
		}
		q.Body = []byte{tab(r.u32(), op == CMSGGuildBankLogQuery)}
		if op == CMSGGuildBankSetTabText {
			q.Body = append(q.Body, guildCString(r.str(r.bits(14)))...)
		}
	default:
		if op >= CMSGAutoGuildBankItem && op <= CMSGSplitGuildBankItem {
			q.Opcode = 0x3E9
			bankGUID()
			q.Body = append(q.Body, parseGuildBankMove(r, op)...)
		} else {
			r.fail("unknown request opcode")
		}
	}
	if err = r.done(); err != nil {
		return GuildRequest{}, err
	}
	return q, nil
}

func parseGuildBankMove(r *guildReader, op uint16) []byte {
	t, s := r.u8(), r.u8()
	if t >= 6 || (s >= 98 && !(s == 255 && op == CMSGAutoGuildBankItem)) {
		r.fail("invalid bank position")
	}
	bankOnly := op == CMSGMoveGuildBankItem || op == CMSGSwapGuildBankItem || op == CMSGMergeGuildBankItem || op == CMSGSplitGuildBankItem
	if bankOnly {
		t1, s1 := r.u8(), r.u8()
		if t1 >= 6 || s1 >= 98 {
			r.fail("invalid second bank position")
		}
		count := uint32(0)
		if op == CMSGMergeGuildBankItem || op == CMSGSplitGuildBankItem {
			count = r.u32()
		}
		// Modern sends source then destination; WotLK sends destination first.
		b := []byte{1, t1, s1}
		b = binary.LittleEndian.AppendUint32(b, 0)
		b = append(b, t, s)
		b = binary.LittleEndian.AppendUint32(b, 0)
		b = append(b, 0)
		return binary.LittleEndian.AppendUint32(b, count)
	}
	b := []byte{0, t, s}
	b = binary.LittleEndian.AppendUint32(b, 0)
	if op == CMSGAutoStoreGuildBankItem {
		b = append(b, 1)
		b = binary.LittleEndian.AppendUint32(b, 0)
		b = append(b, 1)
		return binary.LittleEndian.AppendUint32(b, 0)
	}
	slot := r.u8()
	count := uint32(0)
	if op == CMSGMergeItemWithGuildBankItem || op == CMSGSplitItemToGuildBank || op == CMSGMergeGuildBankItemWithItem || op == CMSGSplitGuildBankItemToInventory {
		count = r.u32()
	}
	bag := byte(255)
	if r.bits(1) != 0 {
		bag = r.u8()
	}
	// Main-container and bag-equipment indexes share the inventory slot mapping.
	if bag == 255 {
		slot = AdjustInventorySlot(slot)
	} else {
		bag = AdjustInventorySlot(bag)
	}
	toChar := byte(0)
	if op == CMSGStoreGuildBankItem || op == CMSGMergeGuildBankItemWithItem || op == CMSGSplitGuildBankItemToInventory {
		toChar = 1
	}
	b = append(b, 0, bag, slot, toChar)
	return binary.LittleEndian.AppendUint32(b, count)
}
