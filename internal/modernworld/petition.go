package modernworld

import (
	"encoding/binary"
	"fmt"
)

// 3.4.3.54261 opcodes. Petition IDs are independent of charter item counters.
const (
	CMSGQueryPetition               uint16 = 0x3277
	CMSGOfferPetition               uint16 = 0x32FD
	CMSGPetitionBuy                 uint16 = 0x34C8
	CMSGPetitionShowSignatures      uint16 = 0x34C9
	CMSGSignPetition                uint16 = 0x3533
	CMSGDeclinePetition             uint16 = 0x3534
	CMSGTurnInPetition              uint16 = 0x3535
	CMSGPetitionRenameGuild         uint16 = 0x36D1
	SMSGQueryPetitionResponse       uint16 = 0x291B
	SMSGPetitionShowList            uint16 = 0x26BF
	SMSGPetitionShowSignatures      uint16 = 0x26C0
	SMSGPetitionSignResults         uint16 = 0x274C
	SMSGPetitionRenameGuildResponse uint16 = 0x29FA
	SMSGTurnInPetitionResult        uint16 = 0x274E
)

type PetitionGUIDKind byte

const (
	PetitionItem PetitionGUIDKind = iota
	PetitionNPC
	PetitionPlayer
)

func IsPetitionClientOpcode(op uint16) bool {
	switch op {
	case CMSGQueryPetition, CMSGOfferPetition, CMSGPetitionBuy, CMSGPetitionShowSignatures, CMSGSignPetition, CMSGDeclinePetition, CMSGTurnInPetition, CMSGPetitionRenameGuild:
		return true
	}
	return false
}

// ParsePetitionRequest validates complete messages before they may be forwarded.
func ParsePetitionRequest(op uint16, body []byte, resolve func(GUID128, PetitionGUIDKind) (uint64, bool)) (Packet, error) {
	r := guildRead(body)
	q := Packet{}
	guid := func(kind PetitionGUIDKind) {
		g := r.guid()
		id, ok := resolve(g, kind)
		if !ok || id == 0 {
			r.fail("unknown petition GUID")
		}
		q.Body = binary.LittleEndian.AppendUint64(q.Body, id)
	}
	switch op {
	case CMSGPetitionBuy:
		q.Opcode = 0x1BD
		n := r.bits(7)
		guid(PetitionNPC)
		index := r.u32()
		title := r.str(n)
		q.Body = append(q.Body, make([]byte, 12)...)
		q.Body = append(q.Body, guildCString(title)...)
		// Body string, seven uint32s, uint16 gender, three uint32s, ten strings.
		q.Body = append(q.Body, make([]byte, 1+28+2+12+10)...)
		q.Body = binary.LittleEndian.AppendUint32(q.Body, index)
		q.Body = binary.LittleEndian.AppendUint32(q.Body, 0)
	case CMSGQueryPetition:
		q.Opcode = 0x1C6
		q.Body = guildU32(r.u32())
		guid(PetitionItem)
	case CMSGPetitionShowSignatures:
		q.Opcode = 0x1BE
		guid(PetitionItem)
	case CMSGOfferPetition:
		q.Opcode = 0x1C3
		q.Body = guildU32(r.u32())
		guid(PetitionItem)
		guid(PetitionPlayer)
	case CMSGSignPetition:
		q.Opcode = 0x1C0
		guid(PetitionItem)
		q.Body = append(q.Body, r.u8())
	case CMSGDeclinePetition:
		q.Opcode = 0x1C2
		guid(PetitionItem)
	case CMSGPetitionRenameGuild:
		q.Opcode = 0x2C1
		guid(PetitionItem)
		q.Body = append(q.Body, guildCString(r.str(r.bits(7)))...)
	case CMSGTurnInPetition:
		q.Opcode = 0x1C4
		guid(PetitionItem)
		// Classic accepts both the guild-only form and the five arena emblem fields.
		// AC uses these only for arena charters; guild emblems remain vendor-managed.
		if r.r.remaining() != 0 {
			for i := 0; i < 5; i++ {
				q.Body = binary.LittleEndian.AppendUint32(q.Body, r.u32())
			}
		} else {
			q.Body = append(q.Body, make([]byte, 20)...)
		}
	default:
		return Packet{}, fmt.Errorf("unknown petition request %#x", op)
	}
	if err := r.done(); err != nil {
		return Packet{}, err
	}
	return q, nil
}

func IsPetitionServerOpcode(op uint16) bool {
	switch op {
	case 0x1BC, 0x1BF, 0x1C1, 0x1C2, 0x1C5, 0x1C7, 0x2C1:
		return true
	}
	return false
}

// TranslateLegacyPetition has no session mutations; malformed packets emit nothing.
func TranslateLegacyPetition(op uint16, body []byte, realm uint32, resolve, account func(uint64) GUID128, name func(uint64) string) (Packet, error) {
	r := guildRead(body)
	q := Packet{}
	guid := func() { q.Body = guildAppendGUID(q.Body, resolve(r.u64())) }
	u32 := func() { q.Body = binary.LittleEndian.AppendUint32(q.Body, r.u32()) }
	switch op {
	case 0x1BC:
		q.Opcode = SMSGPetitionShowList
		guid()
		n := int(r.u8())
		q.Body = binary.LittleEndian.AppendUint32(q.Body, uint32(n))
		for i := 0; i < n; i++ {
			index, entry := r.u32(), r.u32()
			r.u32() // legacy display ID
			cost := r.u32()
			r.u32()
			signs := r.u32()
			arena := uint32(0)
			if entry != 5863 {
				arena = 1
			}
			for _, v := range []uint32{index, cost, entry, arena, signs} {
				q.Body = binary.LittleEndian.AppendUint32(q.Body, v)
			}
		}
	case 0x1BF:
		q.Opcode = SMSGPetitionShowSignatures
		guid()
		owner := r.u64()
		q.Body = guildAppendGUID(q.Body, resolve(owner))
		q.Body = guildAppendGUID(q.Body, account(owner))
		u32()
		n := int(r.u8())
		q.Body = binary.LittleEndian.AppendUint32(q.Body, uint32(n))
		for i := 0; i < n; i++ {
			guid()
			u32()
		}
	case 0x1C7:
		q.Opcode = SMSGQueryPetitionResponse
		id := r.u32()
		owner := r.u64()
		title, text := r.cstr(127), r.cstr(4095)
		q.Body = append(guildU32(id), 0x80) // Allow; flushed before PetitionInfo
		q.Body = binary.LittleEndian.AppendUint32(q.Body, id)
		q.Body = guildAppendGUID(q.Body, resolve(owner))
		for i := 0; i < 7; i++ {
			u32()
		}
		gender, e := r.r.u16()
		guildValue(r, gender, e)
		q.Body = binary.LittleEndian.AppendUint16(q.Body, gender)
		for i := 0; i < 3; i++ {
			u32()
		}
		var choices [10]string
		for i := range choices {
			choices[i] = r.cstr(63)
		}
		muid, staticType := r.u32(), r.u32()
		q.Body = binary.LittleEndian.AppendUint32(q.Body, staticType)
		q.Body = binary.LittleEndian.AppendUint32(q.Body, muid)
		w := newBitWriter(q.Body)
		w.writeBits(uint32(len(title)), 7)
		w.writeBits(uint32(len(text)), 12)
		for _, s := range choices {
			w.writeBits(uint32(len(s)), 6)
		}
		q.Body = w.flush()
		for _, s := range choices {
			q.Body = append(q.Body, s...)
		}
		q.Body = append(q.Body, title...)
		q.Body = append(q.Body, text...)
	case 0x2C1:
		q.Opcode = SMSGPetitionRenameGuildResponse
		guid()
		q.Body = guildStrings(q.Body, []int{7}, r.cstr(127))
	case 0x1C1, 0x1C5:
		q.Opcode = SMSGTurnInPetitionResult
		if op == 0x1C1 {
			q.Opcode = SMSGPetitionSignResults
			guid()
			guid()
		}
		result := r.u32()
		if result > 15 {
			r.fail("petition result exceeds four bits")
		}
		w := newBitWriter(q.Body)
		w.writeBits(result, 4)
		q.Body = w.flush()
	case 0x1C2:
		player := r.u64()
		who := name(player)
		text := "对方拒绝签署你的公会登记表。"
		if who != "" {
			text = who + "拒绝签署你的公会登记表。"
		}
		q = Packet{Opcode: SMSGChat, Body: EncodeChatMessage(GUID128{}, GUID128{}, LegacyChatMessage{Type: legacyChatSystem, Text: text}, realm)}
	default:
		return Packet{}, fmt.Errorf("unknown legacy petition opcode %#x", op)
	}
	if err := r.done(); err != nil {
		return Packet{}, err
	}
	return q, nil
}
