package modernworld

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"
)

func guildTestResolve(id uint64) GUID128 { return ModernGUIDForLegacy(id, 0) }
func guildTestRequestFixtures() map[uint16][]byte {
	g := guildAppendGUID(nil, GUID128{Low: 7, High: 1})
	f := map[uint16][]byte{}
	for _, op := range []uint16{CMSGAcceptGuildInvite, CMSGGuildDeclineInvitation, CMSGGuildAutoDeclineInvitation, CMSGGuildLeave, CMSGGuildDelete, CMSGGuildGetRoster, CMSGGuildPermissionsQuery, CMSGGuildBankRemainingWithdrawQuery, CMSGGuildEventLogQuery} {
		f[op] = nil
	}
	for _, op := range []uint16{CMSGGuildPromoteMember, CMSGGuildDemoteMember, CMSGGuildOfficerRemoveMember, CMSGGuildGetRanks, CMSGTabardVendorActivate} {
		f[op] = append([]byte(nil), g...)
	}
	f[CMSGDeclineGuildInvites] = []byte{1}
	f[CMSGGuildDeleteRank] = guildU32(4)
	f[CMSGGuildSetMemberNote] = append(append([]byte(nil), g...), 0x03, 0x80, 'n', 'o', 't')
	f[CMSGGuildUpdateMOTDText] = guildStrings(nil, []int{11}, "欢迎")
	f[CMSGGuildUpdateInfoText] = guildStrings(nil, []int{11}, "info")
	f[CMSGGuildSetGuildMaster] = guildStrings(nil, []int{9}, "Leader")
	f[CMSGGuildInviteByName] = []byte{0x02, 0x80, 'A', 'l', 'i', 'c', 'e'} // 9-bit length=5, arena=false
	f[CMSGGuildAddRank] = append([]byte{8, 4, 0, 0, 0}, []byte("Rank")...)
	rank := []byte{}
	for _, v := range []uint32{4, 4, 0x1234, 100, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 0xFF} {
		rank = binary.LittleEndian.AppendUint32(rank, v)
	}
	f[CMSGGuildSetRankPermissions] = guildStrings(rank, []int{7}, "R")
	f[CMSGQueryGuildInfo] = append(append([]byte(nil), g...), g...)
	f[CMSGSaveGuildEmblem] = append(append([]byte(nil), g...), make([]byte, 20)...)
	f[CMSGGuildBankActivate] = append(append([]byte(nil), g...), 0x80)
	f[CMSGGuildBankBuyTab] = append(append([]byte(nil), g...), 2)
	f[CMSGGuildBankQueryTab] = append(append([]byte(nil), g...), 2, 0x80)
	for _, op := range []uint16{CMSGGuildBankDepositMoney, CMSGGuildBankWithdrawMoney} {
		f[op] = binary.LittleEndian.AppendUint64(append([]byte(nil), g...), 100)
	}
	f[CMSGGuildBankUpdateTab] = guildStrings(append(append([]byte(nil), g...), 2), []int{7, 9}, "Tab", "Icon")
	f[CMSGGuildBankLogQuery] = guildU32(6)
	f[CMSGGuildBankTextQuery] = guildU32(2)
	f[CMSGGuildBankSetTabText] = guildStrings(guildU32(2), []int{14}, "页签说明")
	for op := CMSGAutoGuildBankItem; op <= CMSGSplitGuildBankItem; op++ {
		b := append(append([]byte(nil), g...), 1, 4)
		switch op {
		case CMSGAutoStoreGuildBankItem:
		case CMSGMoveGuildBankItem, CMSGSwapGuildBankItem:
			b = append(b, 2, 5)
		case CMSGMergeGuildBankItem, CMSGSplitGuildBankItem:
			b = binary.LittleEndian.AppendUint32(append(b, 2, 5), 3)
		default:
			b = append(b, 35)
			if op == CMSGMergeItemWithGuildBankItem || op == CMSGSplitItemToGuildBank || op == CMSGMergeGuildBankItemWithItem || op == CMSGSplitGuildBankItemToInventory {
				b = binary.LittleEndian.AppendUint32(b, 3)
			}
			b = append(b, 0)
		}
		f[op] = b
	}
	return f
}

func TestGuildEveryClientOpcodeAndTruncation(t *testing.T) {
	f := guildTestRequestFixtures()
	for op := uint16(0); op < 0xFFFF; op++ {
		if IsGuildClientOpcode(op) {
			if _, ok := f[op]; !ok {
				t.Fatalf("missing fixture %x", op)
			}
		}
	}
	for op, b := range f {
		t.Run(fmt.Sprintf("%04x", op), func(t *testing.T) {
			q, e := ParseGuildRequest(op, b, func(GUID128) uint64 { return 7 })
			if e != nil {
				t.Fatal(e)
			}
			if q.Opcode == 0 && q.BlockInvites == nil {
				t.Fatal("no legacy route")
			}
			for i := 0; i < len(b); i++ {
				if _, e := ParseGuildRequest(op, b[:i], func(GUID128) uint64 { return 7 }); e == nil {
					t.Fatalf("accepted truncation %d/%d", i, len(b))
				}
			}
			if _, e := ParseGuildRequest(op, append(append([]byte(nil), b...), 0), func(GUID128) uint64 { return 7 }); e == nil {
				t.Fatal("accepted trailing byte")
			}
		})
	}
}

func TestGuildBankMoveDirectionsAndMoney(t *testing.T) {
	f := guildTestRequestFixtures()
	for _, op := range []uint16{CMSGMoveGuildBankItem, CMSGSwapGuildBankItem, CMSGMergeGuildBankItem, CMSGSplitGuildBankItem} {
		q, e := ParseGuildRequest(op, f[op], func(GUID128) uint64 { return 7 })
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(q.Body[8:15], []byte{1, 2, 5, 0, 0, 0, 0}) || !bytes.Equal(q.Body[15:17], []byte{1, 4}) {
			t.Fatalf("source/destination not reversed: %x", q.Body)
		}
	}
	for _, op := range []uint16{CMSGAutoGuildBankItem, CMSGStoreGuildBankItem, CMSGMergeItemWithGuildBankItem, CMSGSplitItemToGuildBank, CMSGMergeGuildBankItemWithItem, CMSGSplitGuildBankItemToInventory} {
		q, e := ParseGuildRequest(op, f[op], func(GUID128) uint64 { return 7 })
		if e != nil {
			t.Fatal(e)
		}
		want := byte(0)
		if op == CMSGStoreGuildBankItem || op == CMSGMergeGuildBankItemWithItem || op == CMSGSplitGuildBankItemToInventory {
			want = 1
		}
		if q.Body[16] != 255 || q.Body[17] != 23 || q.Body[18] != want {
			t.Fatalf("op %x wrong bag,slot,direction: %x", op, q.Body)
		}
	}
	q, e := ParseGuildRequest(CMSGAutoStoreGuildBankItem, f[CMSGAutoStoreGuildBankItem], func(GUID128) uint64 { return 7 })
	if e != nil || len(q.Body) != 25 {
		t.Fatalf("auto store WotLK tail %x %v", q.Body, e)
	}
	for _, op := range []uint16{CMSGGuildBankDepositMoney, CMSGGuildBankWithdrawMoney} {
		b := append([]byte(nil), f[op]...)
		binary.LittleEndian.PutUint64(b[len(b)-8:], 1<<32)
		if _, e := ParseGuildRequest(op, b, func(GUID128) uint64 { return 7 }); e == nil {
			t.Fatal("silently truncated money")
		}
	}
}

func TestGuildClientNamesNotesAndRankPermissions(t *testing.T) {
	f := guildTestRequestFixtures()
	parse := func(op uint16) GuildRequest {
		q, e := ParseGuildRequest(op, f[op], func(GUID128) uint64 { return 7 })
		if e != nil {
			t.Fatal(e)
		}
		return q
	}
	if q := parse(CMSGGuildInviteByName); q.Opcode != 0x82 || string(q.Body) != "Alice\x00" {
		t.Fatalf("invite %#v", q)
	}
	if q := parse(CMSGGuildSetMemberNote); q.Opcode != 0x234 || !q.NeedsName || string(q.Body) != "not\x00" {
		t.Fatalf("public note %#v", q)
	}
	if q := parse(CMSGGuildSetRankPermissions); q.Opcode != 0x231 || len(q.Body) != 62 || binary.LittleEndian.Uint32(q.Body[4:]) != 0x1234 || string(q.Body[8:10]) != "R\x00" || binary.LittleEndian.Uint32(q.Body[10:]) != 100 || binary.LittleEndian.Uint32(q.Body[58:]) != 12 {
		t.Fatalf("rank layout %x", q.Body)
	}
	if _, e := ParseGuildRequest(CMSGGuildUpdateInfoText, []byte{0, 32, 0}, func(GUID128) uint64 { return 7 }); e == nil {
		t.Fatal("embedded NUL accepted")
	}
}

func guildTestRoster() []byte {
	b := guildU32(2)
	b = append(b, []byte("Hi\x00Info\x00")...)
	b = binary.LittleEndian.AppendUint32(b, 2)
	for i := 0; i < 2; i++ {
		for j := 0; j < 14; j++ {
			b = binary.LittleEndian.AppendUint32(b, uint32(i*100+j))
		}
	}
	for i := uint64(1); i <= 2; i++ {
		b = binary.LittleEndian.AppendUint64(b, i)
		b = append(b, byte(2-i))
		b = append(b, []byte(fmt.Sprintf("Member%d\x00", i))...)
		b = binary.LittleEndian.AppendUint32(b, uint32(i-1))
		b = append(b, 80, 6, 1)
		b = binary.LittleEndian.AppendUint32(b, 1519)
		if i == 2 {
			b = binary.LittleEndian.AppendUint32(b, 0x3FC00000)
		}
		b = append(b, []byte("Note\x00Secret\x00")...)
	}
	return b
}
func guildTestQuery() []byte {
	b := guildU32(9)
	b = append(b, []byte("Guild\x00Master\x00Member\x00")...)
	b = append(b, make([]byte, 8)...)
	for i := uint32(1); i <= 5; i++ {
		b = binary.LittleEndian.AppendUint32(b, i)
	}
	return binary.LittleEndian.AppendUint32(b, 2)
}
func guildTestServerFixtures() map[uint16][]byte {
	f := map[uint16][]byte{0x55: guildTestQuery(), 0x83: []byte("Alice\x00Guild\x00"), 0x86: []byte("Alice\x00"), 0x88: append([]byte("Guild\x00"), make([]byte, 12)...), 0x8A: guildTestRoster(), 0x92: []byte{2, 1, 'H', 'i', 0}, 0x93: append(append(guildU32(1), []byte("Name\x00")...), guildU32(8)...), 0x1F1: guildU32(3), 0x1F2: binary.LittleEndian.AppendUint64(nil, 7), 0x3FE: guildU32(0xFFFFFFFF), 0x40A: []byte{2, 'T', 0}, 0x3FF: {0}, 0x3EE: {6, 0}}
	perms := guildU32(1)
	perms = append(perms, guildU32(0x1234)...)
	perms = append(perms, guildU32(100)...)
	perms = append(perms, 2)
	for i := uint32(0); i < 12; i++ {
		perms = append(perms, guildU32(i)...)
	}
	f[0x3FD] = perms
	// Full update on a nonzero tab, with an explicitly cleared slot.
	bank := binary.LittleEndian.AppendUint64(nil, 1<<40)
	bank = append(bank, 2)
	bank = append(bank, guildU32(5)...)
	bank = append(bank, 1, 1, 3)
	bank = append(bank, guildU32(0)...)
	f[0x3E8] = bank
	return f
}

func TestGuildEveryServerOpcodeAndTruncation(t *testing.T) {
	f := guildTestServerFixtures()
	for op := uint16(0); op < 0xFFFF; op++ {
		if IsGuildServerOpcode(op) {
			if _, ok := f[op]; !ok {
				t.Fatalf("missing server fixture %x", op)
			}
		}
	}
	for op, b := range f {
		t.Run(fmt.Sprintf("%04x", op), func(t *testing.T) {
			state := GuildState{ID: 9}
			if _, e := TranslateLegacyGuild(op, b, &state, 1, GUID128{}, guildTestResolve); e != nil {
				t.Fatal(e)
			}
			for i := 0; i < len(b); i++ {
				if op == 0x55 && i == len(b)-4 {
					continue
				}
				empty := GuildState{ID: 9}
				if out, e := TranslateLegacyGuild(op, b[:i], &empty, 1, GUID128{}, guildTestResolve); e == nil || len(out) != 0 {
					t.Fatalf("accepted truncated %d/%d: %v", i, len(b), e)
				}
				if !reflect.DeepEqual(empty, GuildState{ID: 9}) {
					t.Fatal("truncated input mutated caches")
				}
			}
			empty := GuildState{}
			if out, e := TranslateLegacyGuild(op, append(append([]byte(nil), b...), 0), &empty, 1, GUID128{}, guildTestResolve); e == nil || len(out) != 0 {
				t.Fatal("accepted trailing bytes")
			}
		})
	}
}

func TestGuildRosterOfflineAndRankCache(t *testing.T) {
	s := GuildState{ID: 9, CreateDate: 0x12345678, NumAccounts: 1}
	if _, e := TranslateLegacyGuild(0x55, guildTestQuery(), &s, 1, GUID128{}, guildTestResolve); e != nil {
		t.Fatal(e)
	}
	legacyRoster := bytes.Replace(guildTestRoster(), []byte("Hi\x00Info\x00"), []byte("欢迎\x00公会说明\n第二行\x00"), 1)
	out, e := TranslateLegacyGuild(0x8A, legacyRoster, &s, 1, GUID128{}, guildTestResolve, func(id uint64) byte { return byte(id + 1) })
	if e != nil {
		t.Fatal(e)
	}
	if len(out) != 2 || out[0].Opcode != SMSGGuildRanks || out[1].Opcode != SMSGGuildRoster || s.Ranks[1].Name != "Member" || s.Members[2].LastSave != 0x3FC00000 {
		t.Fatalf("roster cache %#v %v", s, e)
	}
	r := guildRead(out[1].Body)
	if r.u32() != 1 || r.u32() != 0x12345678 || r.u32() != 2 || r.u32() != 2 || r.bits(11) != uint32(len("欢迎")) || r.bits(11) != uint32(len("公会说明\n第二行")) {
		t.Fatal("roster header mismatch")
	}
	for i := uint64(1); i <= 2; i++ {
		if r.guid() != guildTestResolve(i) || r.u32() != uint32(i-1) || r.u32() != 1519 || r.u32() != 0xFFFFFFFF || r.u32() != 0xFFFFFFFF {
			t.Fatal("member scalars mismatch")
		}
		last := r.u32()
		if (i == 2) != (last == 0x3FC00000) {
			t.Fatal("offline last save mismatch")
		}
		for j := 0; j < 6; j++ {
			if r.u32() != 0 {
				t.Fatal("profession placeholder")
			}
		}
		if r.u32() != 1 || r.u8() != byte(2-i) || r.u8() != 80 || r.u8() != 6 || r.u8() != 1 {
			t.Fatal("member status mismatch")
		}
		if r.u64() != i || r.u8() != byte(i+1) {
			t.Fatal("54261 club member ID/race missing")
		}
		n, note, officer := r.bits(6), r.bits(8), r.bits(8)
		if (r.bits(1) != 0) != (i == 1) || r.bits(1) != 0 {
			t.Fatal("authenticated bit")
		}
		if r.str(n) != fmt.Sprintf("Member%d", i) || r.str(note) != "Note" || r.str(officer) != "Secret" {
			t.Fatal("notes mismatch")
		}
	}
	if r.str(uint32(len("欢迎"))) != "欢迎" || r.str(uint32(len("公会说明\n第二行"))) != "公会说明\n第二行" || r.done() != nil {
		t.Fatal("roster framing mismatch")
	}
}

func TestGuildBankPermissionsUnlimitedAndEmptySlot(t *testing.T) {
	f := guildTestServerFixtures()
	s := GuildState{}
	out, e := TranslateLegacyGuild(0x3FD, f[0x3FD], &s, 1, GUID128{}, guildTestResolve)
	if e != nil {
		t.Fatal(e)
	}
	r := guildRead(out[0].Body)
	for _, want := range []uint32{1, 100, 0x1234, 2, 6, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11} {
		if v := r.u32(); v != want {
			t.Fatalf("permissions %d want %d", v, want)
		}
	}
	if r.done() != nil {
		t.Fatal("permissions trailing")
	}
	out, e = TranslateLegacyGuild(0x3FE, f[0x3FE], &s, 1, GUID128{}, guildTestResolve)
	if e != nil || !bytes.Equal(out[0].Body, bytes.Repeat([]byte{255}, 8)) {
		t.Fatal("unlimited must sign extend")
	}
	out, e = TranslateLegacyGuild(0x3E8, f[0x3E8], &s, 1, GUID128{}, guildTestResolve)
	if e != nil {
		t.Fatal(e)
	}
	r = guildRead(out[0].Body)
	if r.u64() != 1<<40 || r.u32() != 2 || r.u32() != 5 || r.u32() != 0 || r.u32() != 1 || r.bits(1) != 1 {
		t.Fatal("full-update flag on nonzero tab lost")
	}
	if r.u32() != 3 {
		t.Fatal("cleared slot lost")
	}
	for i := 0; i < 8; i++ {
		if r.u32() != 0 {
			t.Fatal("empty item not zero")
		}
	}
	r.u8()
	r.u8()
	r.u8()
	if r.done() != nil {
		t.Fatal(r.err)
	}
}

func TestGuildEventsAndLogs(t *testing.T) {
	state := GuildState{ID: 9, Members: map[uint64]GuildMember{1: {Name: "A"}, 2: {Name: "B"}}, Ranks: []GuildRank{{Name: "Master"}}}
	events := map[byte][]string{0: {"A", "B", "Master"}, 1: {"A", "B", "Master"}, 2: {"Welcome"}, 3: {"B"}, 4: {"B"}, 5: {"B", "A"}, 6: {"A"}, 7: {"A", "B"}, 8: {}, 9: {}, 10: {}, 11: {}, 12: {"B"}, 13: {"B"}, 14: {}, 15: {}, 16: {"4", "Tab", "Icon"}, 17: {"0000010000000001"}, 18: {}, 19: {"5"}}
	for typ, ss := range events {
		copyState := state
		b := []byte{typ, byte(len(ss))}
		for _, s := range ss {
			b = append(b, guildCString(s)...)
		}
		if typ == 3 || typ == 4 || typ == 12 || typ == 13 {
			b = binary.LittleEndian.AppendUint64(b, 2)
		}
		out, e := TranslateLegacyGuild(0x92, b, &copyState, 1, guildTestResolve(1), guildTestResolve)
		if e != nil {
			t.Fatalf("event %d: %v", typ, e)
		}
		if typ == 16 && (out[0].Opcode != 0x29F5 || binary.LittleEndian.Uint32(out[0].Body) != 4) {
			t.Fatal("tab event index lost")
		}
		if typ == 17 && binary.LittleEndian.Uint64(out[0].Body) != 0x10000000001 {
			t.Fatal("money narrowed to 32 bits")
		}
	}
	// AzerothCore log stack count is uint32 (Hermes incorrectly reads a byte).
	b := []byte{1, 1, 3}
	b = binary.LittleEndian.AppendUint64(b, 2)
	b = append(b, guildU32(123)...)
	b = append(b, guildU32(1000)...)
	b = append(b, 4)
	b = append(b, guildU32(60)...)
	out, e := TranslateLegacyGuild(0x3EE, b, &state, 1, GUID128{}, guildTestResolve)
	if e != nil {
		t.Fatal(e)
	}
	r := guildRead(out[0].Body)
	r.u32()
	r.u32()
	r.u8()
	if r.guid() != guildTestResolve(2) || r.u32() != 60 || r.u8() != 3 || r.u8() != 0x70 || r.u32() != 123 || r.u32() != 1000 || r.u8() != 4 || r.done() != nil {
		t.Fatal("bank log layout")
	}
	// All six WotLK event-log shapes, particularly leave=6 and promote=3.
	b = []byte{6}
	for typ := byte(1); typ <= 6; typ++ {
		b = append(b, typ)
		b = binary.LittleEndian.AppendUint64(b, 1)
		if typ != 2 && typ != 6 {
			b = binary.LittleEndian.AppendUint64(b, 2)
		}
		if typ == 3 || typ == 4 {
			b = append(b, 4)
		}
		b = append(b, guildU32(60)...)
	}
	out, e = TranslateLegacyGuild(0x3FF, b, &state, 1, GUID128{}, guildTestResolve)
	if e != nil || len(out) != 1 {
		t.Fatalf("event log %v", e)
	}
}

func TestGuildBankPopulatedTabRandomPropertyAndGem(t *testing.T) {
	b := binary.LittleEndian.AppendUint64(nil, 12345)
	b = append(b, 0)
	b = append(b, guildU32(4)...)
	b = append(b, 1, 1)
	b = append(b, []byte("Materials\x00Icon\x00")...)
	b = append(b, 1, 5)
	for _, v := range []uint32{30531, 0x10, 0xFFFFFFFE, 123, 20, 2673} {
		b = append(b, guildU32(v)...)
	}
	b = append(b, 7, 1, 0)
	b = append(b, guildU32(3054)...)
	out, e := TranslateLegacyGuild(0x3E8, b, &GuildState{}, 1, GUID128{}, guildTestResolve)
	if e != nil {
		t.Fatal(e)
	}
	r := guildRead(out[0].Body)
	if r.u64() != 12345 || r.u32() != 0 || r.u32() != 4 || r.u32() != 1 || r.u32() != 1 || r.bits(1) != 1 {
		t.Fatal("populated bank header")
	}
	if r.u32() != 0 {
		t.Fatal("tab index")
	}
	n, icon := r.bits(7), r.bits(9)
	if r.str(n) != "Materials" || r.str(icon) != "Icon" {
		t.Fatal("tab labels")
	}
	for _, want := range []uint32{5, 20, 2673, 7, 0, 0x10, 30531, 123, 0xFFFFFFFE} {
		if r.u32() != want {
			t.Fatalf("item field want %x", want)
		}
	}
	if r.u8() != 0 || r.u8() != 0 || r.bits(2) != 1 || r.bits(1) != 0 || r.u8() != 0 || r.u32() != 30555 || r.u32() != 0 || r.u32() != 0 || r.u8() != 0 || r.u8() != 0 || r.done() != nil {
		t.Fatalf("gem ItemInstance layout %x", out[0].Body)
	}
	for i := 0; i < len(b); i++ {
		if out, e := TranslateLegacyGuild(0x3E8, b[:i], &GuildState{}, 1, GUID128{}, guildTestResolve); e == nil || len(out) > 0 {
			t.Fatalf("accepted truncated populated bank at %d", i)
		}
	}
}

func TestGuildBoundsAndExplicitContainer(t *testing.T) {
	f := guildTestRequestFixtures()
	b := append([]byte(nil), f[CMSGAutoGuildBankItem]...)
	b[len(b)-1] = 128
	b = append(b, 30)
	q, e := ParseGuildRequest(CMSGAutoGuildBankItem, b, func(GUID128) uint64 { return 7 })
	if e != nil || q.Body[16] != 19 || q.Body[17] != 35 {
		t.Fatal("bag equipment slot conversion changed contained slot")
	}
	for _, v := range []struct {
		op uint16
		b  []byte
	}{{CMSGGuildBankLogQuery, guildU32(7)}, {CMSGGuildBankTextQuery, guildU32(6)}, {CMSGGuildDeleteRank, guildU32(10)}, {CMSGDeclineGuildInvites, []byte{2}}} {
		if _, e := ParseGuildRequest(v.op, v.b, func(GUID128) uint64 { return 7 }); e == nil {
			t.Fatalf("invalid %x accepted", v.op)
		}
	}
	for _, b := range [][]byte{guildU32(10001), append(guildU32(0), []byte{0, 0, 11, 0, 0, 0}...)} {
		if out, e := TranslateLegacyGuild(0x8A, b, &GuildState{}, 1, GUID128{}, guildTestResolve); e == nil || len(out) > 0 {
			t.Fatal("unbounded roster")
		}
	}
}

func TestGuildMembershipValuesJoinAndLeave(t *testing.T) {
	for _, id := range []uint32{9, 0} {
		fields := map[int]uint32{legacyPlayerGuildID: id}
		unit := encodeUnitValuesDelta(legacyActivePlayerValues{fields: fields}, fields, 4, 0)
		r := guildRead(unit)
		if r.bits(8) != 8 || r.bits(32) != 0x801 || r.guid() != GuildGUID(id) || r.done() != nil {
			t.Fatalf("guild GUID update mask/value %x", unit)
		}
		player := encodePlayerValuesDelta(legacyActivePlayerValues{fields: fields}, fields, 0, nil, true)
		// PlayerData uses a four-bit block selector and a trailing mask flag.
		r = guildRead(player)
		if r.bits(4) != 1 || r.bits(32) != 0x801 || r.bits(1) != 1 {
			t.Fatalf("guild level mask %x", player)
		}
		want := uint32(0)
		if id != 0 {
			want = 25
		}
		if r.u32() != want || r.done() != nil {
			t.Fatalf("guild level value %x", player)
		}
		body, emitted, e := EncodeValuesUpdate(LegacyObjectUpdate{Type: LegacyUpdateValues, GUID: 1, Values: LegacyUpdateValuesBlock{Fields: fields}}, ValuesUpdateOptions{ObjectType: 4, GUID: guildTestResolve(1), Active: true, Fields: fields})
		if e != nil || emitted != 1 || len(body) == 0 {
			t.Fatalf("membership field not emitted %v", e)
		}
	}
}

func FuzzGuildPackets(f *testing.F) {
	for op, b := range guildTestRequestFixtures() {
		f.Add(op, b)
	}
	for op, b := range guildTestServerFixtures() {
		f.Add(op, b)
	}
	f.Fuzz(func(t *testing.T, op uint16, b []byte) {
		if len(b) > 1<<16 {
			return
		}
		if IsGuildClientOpcode(op) {
			_, _ = ParseGuildRequest(op, b, func(g GUID128) uint64 { return g.Low })
		}
		if IsGuildServerOpcode(op) {
			_, _ = TranslateLegacyGuild(op, b, &GuildState{}, 1, GUID128{}, guildTestResolve)
		}
	})
}
