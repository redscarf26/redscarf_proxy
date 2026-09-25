package modernworld

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

type GuildMember struct {
	GUID                      uint64
	Name, Note, OfficerNote   string
	Rank, Area, LastSave      uint32
	Status, Level, Class, Sex byte
}
type GuildRank struct {
	Name         string
	Flags, Money uint32
	Tabs         [12]uint32
}
type GuildInfo struct {
	ID     uint32
	Name   string
	Ranks  [10]string
	Emblem [5]uint32
}

// GuildState belongs to one character session and is protected by worldMu.
type GuildState struct {
	BlockInvites                bool
	ID, CreateDate, NumAccounts uint32
	Info                        map[uint32]GuildInfo
	Members                     map[uint64]GuildMember
	Ranks                       []GuildRank
}

func IsGuildServerOpcode(op uint16) bool {
	switch op {
	case 0x55, 0x83, 0x86, 0x88, 0x8A, 0x92, 0x93, 0x1F1, 0x1F2, 0x3E8, 0x3EE, 0x3FD, 0x3FE, 0x3FF, 0x40A:
		return true
	}
	return false
}

// TranslateLegacyGuild validates the entire payload before committing caches.
func TranslateLegacyGuild(op uint16, body []byte, state *GuildState, realm uint32, player GUID128, resolve func(uint64) GUID128, raceForMember ...func(uint64) byte) ([]Packet, error) {
	r := guildRead(body)
	var out []Packet
	emit := func(op uint16, b []byte) { out = append(out, Packet{Opcode: op, Body: b}) }
	switch op {
	case 0x55:
		info := GuildInfo{ID: r.u32()}
		info.Name = r.cstr(127)
		for i := range info.Ranks {
			info.Ranks[i] = r.cstr(127)
		}
		for i := range info.Emblem {
			info.Emblem[i] = r.u32()
		}
		if r.r.remaining() == 4 {
			r.count(r.u32(), 10)
		}
		if e := r.done(); e != nil {
			return nil, e
		}
		if state.Info == nil {
			state.Info = map[uint32]GuildInfo{}
		}
		state.Info[info.ID] = info
		b := guildAppendGUID(nil, GuildGUID(info.ID))
		b = append(b, 0x80)
		b = guildAppendGUID(b, GuildGUID(info.ID))
		b = binary.LittleEndian.AppendUint32(b, realm)
		n := uint32(0)
		for _, s := range info.Ranks {
			if s != "" {
				n++
			}
		}
		b = binary.LittleEndian.AppendUint32(b, n)
		for _, v := range info.Emblem {
			b = binary.LittleEndian.AppendUint32(b, v)
		}
		w := newBitWriter(b)
		w.writeBits(uint32(len(info.Name)), 7)
		b = w.flush()
		for i, s := range info.Ranks {
			if s != "" {
				b = binary.LittleEndian.AppendUint32(b, uint32(i))
				b = binary.LittleEndian.AppendUint32(b, uint32(i))
				b = guildStrings(b, []int{7}, s)
			}
		}
		b = append(b, info.Name...)
		emit(SMSGQueryGuildInfoResponse, b)
		if info.ID == state.ID && state.Ranks != nil {
			for i := range state.Ranks {
				state.Ranks[i].Name = info.Ranks[i]
			}
			emit(SMSGGuildRanks, encodeGuildRanks(state.Ranks))
		}
	case 0x88:
		r.cstr(127)
		date := r.u32()
		r.u32()
		accounts := r.u32()
		if e := r.done(); e != nil {
			return nil, e
		}
		state.CreateDate = date
		state.NumAccounts = accounts
	case 0x8A:
		n := r.count(r.u32(), 10000)
		motd, info := r.cstr(2047), r.cstr(2047)
		nr := r.count(r.u32(), 10)
		ranks := make([]GuildRank, nr)
		for i := range ranks {
			ranks[i].Name = state.Info[state.ID].Ranks[i]
			ranks[i].Flags = r.u32()
			ranks[i].Money = r.u32()
			for j := range ranks[i].Tabs {
				ranks[i].Tabs[j] = r.u32()
			}
		}
		members := make([]GuildMember, n)
		for i := range members {
			m := &members[i]
			m.GUID = r.u64()
			m.Status = r.u8()
			m.Name = r.cstr(63)
			m.Rank = r.u32()
			m.Level = r.u8()
			m.Class = r.u8()
			m.Sex = r.u8()
			m.Area = r.u32()
			if m.Status == 0 {
				m.LastSave = r.u32()
			}
			m.Note = r.cstr(255)
			m.OfficerNote = r.cstr(255)
		}
		if e := r.done(); e != nil {
			return nil, e
		}
		state.Ranks = ranks
		state.Members = make(map[uint64]GuildMember, n)
		for _, m := range members {
			state.Members[m.GUID] = m
		}
		emit(SMSGGuildRanks, encodeGuildRanks(ranks))
		accounts := state.NumAccounts
		if accounts == 0 {
			accounts = uint32(n)
		}
		b := guildU32(accounts)
		b = binary.LittleEndian.AppendUint32(b, state.CreateDate)
		b = binary.LittleEndian.AppendUint32(b, 2)
		b = binary.LittleEndian.AppendUint32(b, uint32(n))
		w := newBitWriter(b)
		w.writeBits(uint32(len(motd)), 11)
		w.writeBits(uint32(len(info)), 11)
		b = w.flush()
		for i, m := range members {
			b = guildAppendGUID(b, resolve(m.GUID))
			for _, v := range []uint32{m.Rank, m.Area, 0xFFFFFFFF, 0xFFFFFFFF, m.LastSave} {
				b = binary.LittleEndian.AppendUint32(b, v)
			}
			b = append(b, make([]byte, 24)...)
			b = binary.LittleEndian.AppendUint32(b, realm)
			b = append(b, m.Status, m.Level, m.Class, m.Sex)
			// legacy proxy's GuildRosterMemberData.WriteWlk (0x953f00), unlike
			// Hermes' generic writer, includes ClubMemberID and RaceID here.
			// WotLK has no club identity; use its one-based roster ordinal.
			b = binary.LittleEndian.AppendUint64(b, uint64(i+1))
			race := byte(1)
			if len(raceForMember) != 0 && raceForMember[0] != nil {
				if known := raceForMember[0](m.GUID); known != 0 {
					race = known
				}
			}
			b = append(b, race)
			w = newBitWriter(b)
			w.writeBits(uint32(len(m.Name)), 6)
			w.writeBits(uint32(len(m.Note)), 8)
			w.writeBits(uint32(len(m.OfficerNote)), 8)
			w.writeBit(m.Status != 0)
			w.writeBit(false)
			b = w.flush()
			b = append(b, m.Name...)
			b = append(b, m.Note...)
			b = append(b, m.OfficerNote...)
		}
		b = append(b, motd...)
		b = append(b, info...)
		emit(SMSGGuildRoster, b)
	case 0x93:
		command := r.u32()
		name := r.cstr(255)
		result := r.u32()
		b := guildU32(result)
		b = binary.LittleEndian.AppendUint32(b, command)
		emit(SMSGGuildCommandResult, guildStrings(b, []int{8}, name))
	case 0x83:
		inviter, name := r.cstr(63), r.cstr(127)
		var info GuildInfo
		for _, v := range state.Info {
			if v.Name == name {
				info = v
				break
			}
		}
		w := newBitWriter(nil)
		w.writeBits(uint32(len(inviter)), 6)
		w.writeBits(uint32(len(name)), 7)
		w.writeBits(0, 7)
		b := w.flush()
		b = binary.LittleEndian.AppendUint32(b, realm)
		b = binary.LittleEndian.AppendUint32(b, realm)
		b = guildAppendGUID(b, GuildGUID(info.ID))
		b = binary.LittleEndian.AppendUint32(b, 0)
		b = guildAppendGUID(b, GUID128{})
		for _, v := range info.Emblem {
			b = binary.LittleEndian.AppendUint32(b, v)
		}
		b = binary.LittleEndian.AppendUint32(b, 0xFFFFFFFF)
		b = append(b, inviter...)
		b = append(b, name...)
		emit(SMSGGuildInvite, b)
	case 0x86:
		name := r.cstr(63)
		w := newBitWriter(nil)
		w.writeBits(uint32(len(name)), 6)
		w.writeBit(false)
		b := binary.LittleEndian.AppendUint32(w.flush(), realm)
		emit(SMSGGuildInviteDeclined, append(b, name...))
	case 0x1F1:
		emit(SMSGPlayerSaveGuildEmblem, guildU32(r.u32()))
	case 0x1F2:
		// 54261 replaced the dedicated window opcode with NPC interaction.
		b := guildAppendGUID(nil, resolve(r.u64()))
		b = binary.LittleEndian.AppendUint32(b, 14)
		emit(0x288A, append(b, 0x80))
	case 0x3FE:
		// -1 is unlimited, not a positive 4294967295 copper allowance.
		emit(SMSGGuildBankRemainingWithdrawMoney, binary.LittleEndian.AppendUint64(nil, uint64(int64(int32(r.u32())))))
	case 0x40A:
		t := r.u8()
		if t >= 6 {
			r.fail("invalid bank tab")
		}
		emit(SMSGGuildBankTextQueryResult, guildStrings(guildU32(uint32(t)), []int{14}, r.cstr(16383)))
	case 0x3FD:
		rank, flags, money := r.u32(), r.u32(), r.u32()
		tabs := r.u8()
		if tabs > 6 {
			r.fail("invalid purchased tab count")
		}
		b := guildU32(rank)
		b = binary.LittleEndian.AppendUint32(b, money)
		b = binary.LittleEndian.AppendUint32(b, flags)
		b = binary.LittleEndian.AppendUint32(b, uint32(tabs))
		b = binary.LittleEndian.AppendUint32(b, 6)
		for i := 0; i < 12; i++ {
			b = binary.LittleEndian.AppendUint32(b, r.u32())
		}
		emit(SMSGGuildPermissionsQueryResults, b)
	case 0x3E8:
		emit(SMSGGuildBankQueryResults, encodeGuildBank(r))
	case 0x3EE:
		emit(SMSGGuildBankLogQueryResults, encodeGuildBankLog(r, resolve))
	case 0x3FF:
		n := int(r.u8())
		b := guildU32(uint32(n))
		for i := 0; i < n; i++ {
			typ := r.u8()
			g := r.u64()
			other := uint64(0)
			rank := byte(0)
			if typ != 2 && typ != 6 {
				other = r.u64()
			}
			if typ == 3 || typ == 4 {
				rank = r.u8()
			}
			date := r.u32()
			b = guildAppendGUID(b, resolve(g))
			b = guildAppendGUID(b, resolve(other))
			b = append(b, typ, rank)
			b = binary.LittleEndian.AppendUint32(b, date)
		}
		emit(SMSGGuildEventLogQueryResults, b)
	case 0x92:
		return translateGuildEvent(r, state, realm, player, resolve)
	default:
		return nil, fmt.Errorf("unknown legacy guild opcode 0x%x", op)
	}
	if e := r.done(); e != nil {
		return nil, e
	}
	return out, nil
}

func encodeGuildRanks(ranks []GuildRank) []byte {
	b := guildU32(uint32(len(ranks)))
	for i, r := range ranks {
		b = append(b, byte(i))
		for _, v := range []uint32{uint32(i), r.Flags, r.Money} {
			b = binary.LittleEndian.AppendUint32(b, v)
		}
		for _, v := range r.Tabs {
			b = binary.LittleEndian.AppendUint32(b, v)
		}
		b = guildStrings(b, []int{7}, r.Name)
	}
	return b
}

func encodeGuildBank(r *guildReader) []byte {
	money := r.u64()
	tab := r.u8()
	remaining := r.u32()
	full := r.u8() != 0
	if tab >= 6 {
		r.fail("invalid bank tab")
	}
	type bankTab struct{ name, icon string }
	var tabs []bankTab
	if full && tab == 0 {
		n := r.count(uint32(r.u8()), 6)
		for i := 0; i < n; i++ {
			tabs = append(tabs, bankTab{r.cstr(127), r.cstr(511)})
		}
	}
	n := r.count(uint32(r.u8()), 98)
	b := binary.LittleEndian.AppendUint64(nil, money)
	for _, v := range []uint32{uint32(tab), remaining, uint32(len(tabs)), uint32(n)} {
		b = binary.LittleEndian.AppendUint32(b, v)
	}
	w := newBitWriter(b)
	w.writeBit(full)
	b = w.flush()
	for i, t := range tabs {
		b = binary.LittleEndian.AppendUint32(b, uint32(i))
		b = guildStrings(b, []int{7, 9}, t.name, t.icon)
	}
	for i := 0; i < n; i++ {
		slot := r.u8()
		if slot >= 98 {
			r.fail("invalid bank slot")
		}
		id := r.u32()
		var flags, property, seed, count, enchant, charges uint32
		type gem struct {
			slot byte
			id   uint32
		}
		var gems []gem
		if id != 0 {
			flags = r.u32()
			property = r.u32()
			if property != 0 {
				seed = r.u32()
			}
			count = r.u32()
			enchant = r.u32()
			charges = uint32(r.u8())
			ng := r.count(uint32(r.u8()), 3)
			for j := 0; j < ng; j++ {
				s, e := r.u8(), r.u32()
				if item := gemItemIDFromEnchant(e); item != 0 {
					gems = append(gems, gem{s, item})
				}
			}
		}
		for _, v := range []uint32{uint32(slot), count, enchant, charges, 0, flags} {
			b = binary.LittleEndian.AppendUint32(b, v)
		}
		b = appendItemInstance(b, id, seed, property)
		w = newBitWriter(b)
		w.writeBits(uint32(len(gems)), 2)
		w.writeBit(false)
		b = w.flush()
		for _, g := range gems {
			b = append(b, g.slot)
			b = appendItemInstance(b, g.id, 0, 0)
		}
	}
	return b
}

func encodeGuildBankLog(r *guildReader, resolve func(uint64) GUID128) []byte {
	tab := r.u8()
	if tab > 6 {
		r.fail("invalid bank log tab")
	}
	n := r.u8()
	b := guildU32(uint32(tab))
	b = binary.LittleEndian.AppendUint32(b, uint32(n))
	b = append(b, 0)
	for i := byte(0); i < n; i++ {
		typ := r.u8()
		g := r.u64()
		var item, count, money uint32
		var other byte
		isItem := typ == 1 || typ == 2 || typ == 3 || typ == 7
		move := typ == 3 || typ == 7
		if isItem {
			item = r.u32()
			count = r.u32()
			if move {
				other = r.u8()
			}
		} else {
			money = r.u32()
		}
		offset := r.u32()
		b = guildAppendGUID(b, resolve(g))
		b = binary.LittleEndian.AppendUint32(b, offset)
		b = append(b, typ)
		w := newBitWriter(b)
		w.writeBit(!isItem)
		w.writeBit(isItem)
		w.writeBit(isItem)
		w.writeBit(move)
		b = w.flush()
		if isItem {
			b = binary.LittleEndian.AppendUint32(b, item)
			b = binary.LittleEndian.AppendUint32(b, count)
			if move {
				b = append(b, other)
			}
		} else {
			b = binary.LittleEndian.AppendUint64(b, uint64(money))
		}
	}
	return b
}

func translateGuildEvent(r *guildReader, state *GuildState, realm uint32, player GUID128, resolve func(uint64) GUID128) ([]Packet, error) {
	typ := r.u8()
	n := r.count(uint32(r.u8()), 3)
	ss := make([]string, n)
	for i := range ss {
		ss[i] = r.cstr(2047)
	}
	var raw uint64
	if r.r.remaining() != 0 {
		raw = r.u64()
	}
	if e := r.done(); e != nil {
		return nil, e
	}
	need := map[byte]int{0: 3, 1: 3, 2: 1, 3: 1, 4: 1, 5: 2, 6: 1, 7: 2, 8: 0, 9: 0, 10: 0, 11: 0, 12: 1, 13: 1, 14: 0, 15: 0, 16: 3, 17: 1, 18: 0, 19: 1}
	min, ok := need[typ]
	if !ok || len(ss) < min {
		return nil, fmt.Errorf("guild event %d has invalid parameters", typ)
	}
	lookup := func(name string) GUID128 {
		for id, m := range state.Members {
			if strings.EqualFold(name, m.Name) {
				return resolve(id)
			}
		}
		return GUID128{}
	}
	g := resolve(raw)
	if raw == 0 && n > 0 {
		g = lookup(ss[0])
	}
	var op uint16
	var b []byte
	person := func(g GUID128) { b = guildAppendGUID(b, g); b = binary.LittleEndian.AppendUint32(b, realm) }
	nameOK := func(s string) bool { return len(s) <= 63 }
	switch typ {
	case 0, 1:
		rank := uint32(0)
		found := false
		for i, v := range state.Ranks {
			if v.Name == ss[2] {
				rank = uint32(i)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("guild rank name unavailable: %q", ss[2])
		}
		op = SMSGGuildSendRankChange
		b = guildAppendGUID(b, lookup(ss[0]))
		b = guildAppendGUID(b, lookup(ss[1]))
		b = binary.LittleEndian.AppendUint32(b, rank)
		w := newBitWriter(b)
		w.writeBit(typ == 0)
		b = w.flush()
	case 2:
		op = 0x29EF
		b = guildStrings(nil, []int{11}, ss[0])
	case 3, 12, 13:
		if !nameOK(ss[0]) {
			return nil, fmt.Errorf("guild event name too long")
		}
		op = 0x29EB
		if typ != 3 {
			op = 0x29F0
		}
		person(g)
		w := newBitWriter(b)
		w.writeBits(uint32(len(ss[0])), 6)
		if typ != 3 {
			w.writeBit(typ == 12)
			w.writeBit(false)
		}
		b = append(w.flush(), ss[0]...)
	case 4, 5:
		if !nameOK(ss[0]) || (typ == 5 && !nameOK(ss[1])) {
			return nil, fmt.Errorf("guild event name too long")
		}
		op = 0x29EC
		w := newBitWriter(nil)
		w.writeBit(typ == 5)
		w.writeBits(uint32(len(ss[0])), 6)
		if typ == 5 {
			w.writeBits(uint32(len(ss[1])), 6)
		}
		b = w.flush()
		if typ == 5 {
			person(lookup(ss[1]))
			b = append(b, ss[1]...)
		}
		person(g)
		b = append(b, ss[0]...)
	case 7:
		if !nameOK(ss[0]) || !nameOK(ss[1]) {
			return nil, fmt.Errorf("guild event name too long")
		}
		op = 0x29ED
		w := newBitWriter(nil)
		w.writeBit(false)
		w.writeBits(uint32(len(ss[0])), 6)
		w.writeBits(uint32(len(ss[1])), 6)
		b = w.flush()
		person(lookup(ss[0]))
		person(lookup(ss[1]))
		b = append(b, ss[0]...)
		b = append(b, ss[1]...)
	case 8:
		op = 0x29EE
	case 10, 11:
		op = 0x29F1
	case 14:
		op = 0x29F8
	case 15:
		op = 0x29F3
	case 16, 19:
		t, e := strconv.ParseUint(ss[0], 10, 32)
		if e != nil || t >= 6 {
			return nil, fmt.Errorf("invalid guild event tab")
		}
		b = guildU32(uint32(t))
		op = 0x29F6
		if typ == 16 {
			if len(ss[1]) > 127 || len(ss[2]) > 511 {
				return nil, fmt.Errorf("guild bank tab strings too long")
			}
			op = 0x29F5
			b = guildStrings(b, []int{7, 9}, ss[1], ss[2])
		}
	case 17:
		money, e := strconv.ParseUint(ss[0], 16, 64)
		if e != nil {
			return nil, e
		}
		op = 0x29F7
		b = binary.LittleEndian.AppendUint64(nil, money)
	case 6, 9, 18:
		return nil, nil // informational leader/tabard/withdraw events; state arrives separately
	}
	if typ == 8 || ((typ == 4 || typ == 5) && g == player) {
		state.ID = 0
		state.Members = nil
		state.Ranks = nil
		state.CreateDate = 0
		state.NumAccounts = 0
	}
	return []Packet{{Opcode: op, Body: b}}, nil
}
