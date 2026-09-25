package modernworld

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

// Independent wire fixtures use a tiny GUID mapping to keep expected bytes readable.
func petitionTestGUID(id uint64) GUID128                               { return GUID128{Low: id} }
func petitionTestResolve(g GUID128, _ PetitionGUIDKind) (uint64, bool) { return g.Low, g.Low != 0 }
func petitionTestAccount(id uint64) GUID128                            { return GUID128{Low: id + 100} }
func petitionTestName(uint64) string                                   { return "签名者" }
func petitionU64(v uint64) []byte                                      { return binary.LittleEndian.AppendUint64(nil, v) }
func petitionWords(vs ...uint32) []byte {
	var b []byte
	for _, v := range vs {
		b = binary.LittleEndian.AppendUint32(b, v)
	}
	return b
}
func petitionJoin(bs ...[]byte) []byte { return bytes.Join(bs, nil) }

type petitionFixture struct {
	op   uint16
	body []byte
	want Packet
}

func petitionRequestFixtures() []petitionFixture {
	item := []byte{1, 0, 7}
	name := []byte("公会")
	return []petitionFixture{
		{CMSGQueryPetition, petitionJoin(petitionWords(123), item), Packet{0x1C6, petitionJoin(petitionWords(123), petitionU64(7))}},
		{CMSGPetitionShowSignatures, item, Packet{0x1BE, petitionU64(7)}},
		{CMSGOfferPetition, petitionJoin(petitionWords(0x1020304), item, []byte{1, 0, 8}), Packet{0x1C3, petitionJoin(petitionWords(0x1020304), petitionU64(7), petitionU64(8))}},
		{CMSGSignPetition, petitionJoin(item, []byte{2}), Packet{0x1C0, petitionJoin(petitionU64(7), []byte{2})}},
		{CMSGDeclinePetition, item, Packet{0x1C2, petitionU64(7)}},
		{CMSGPetitionRenameGuild, petitionJoin(item, []byte{12}, name), Packet{0x2C1, petitionJoin(petitionU64(7), name, []byte{0})}},
		{CMSGTurnInPetition, item, Packet{0x1C4, petitionJoin(petitionU64(7), make([]byte, 20))}},
		{CMSGTurnInPetition, petitionJoin(item, petitionWords(1, 2, 3, 4, 5)), Packet{0x1C4, petitionJoin(petitionU64(7), petitionWords(1, 2, 3, 4, 5))}},
		{CMSGPetitionBuy, petitionJoin([]byte{12, 1, 0, 9}, petitionWords(3), name), Packet{0x1BD, petitionJoin(petitionU64(9), make([]byte, 12), name, make([]byte, 54), petitionWords(3, 0))}},
	}
}

func petitionServerFixtures() []petitionFixture {
	// Nonzero scalar fields, independent petition ID, Chinese strings, and all ten choices.
	query := petitionJoin(petitionWords(123), petitionU64(8), []byte("公会\x00说明\x00"), petitionWords(2, 9, 3, 4, 5, 6, 7), []byte{8, 0}, petitionWords(9, 10, 2), []byte("a\x00bb\x00\x00\x00\x00\x00\x00\x00\x00\x00"), petitionWords(99, 1))
	// Lengths: title=6/7 bits, body=6/12 bits, choices=1,2,0.../6 bits.
	lengths := []byte{0x0c, 0x00, 0xc0, 0x84, 0, 0, 0, 0, 0, 0}
	queryWant := petitionJoin(petitionWords(123), []byte{128}, petitionWords(123), []byte{1, 0, 8}, petitionWords(2, 9, 3, 4, 5, 6, 7), []byte{8, 0}, petitionWords(9, 10, 2, 1, 99), lengths, []byte("abb公会说明"))
	return []petitionFixture{
		{0x1BC, petitionJoin(petitionU64(9), []byte{2}, petitionWords(1, 5863, 16161, 1000, 0, 9, 3, 23562, 22, 8000, 5, 4)), Packet{SMSGPetitionShowList, petitionJoin([]byte{1, 0, 9}, petitionWords(2, 1, 1000, 5863, 0, 9, 3, 8000, 23562, 1, 4))}},
		{0x1BF, petitionJoin(petitionU64(7), petitionU64(8), petitionWords(123), []byte{2}, petitionU64(10), petitionWords(0), petitionU64(11), petitionWords(2)), Packet{SMSGPetitionShowSignatures, petitionJoin([]byte{1, 0, 7, 1, 0, 8, 1, 0, 108}, petitionWords(123, 2), []byte{1, 0, 10}, petitionWords(0), []byte{1, 0, 11}, petitionWords(2))}},
		{0x1C7, query, Packet{SMSGQueryPetitionResponse, queryWant}},
		{0x2C1, petitionJoin(petitionU64(7), []byte("公会\x00")), Packet{SMSGPetitionRenameGuildResponse, []byte{1, 0, 7, 12, 0xe5, 0x85, 0xac, 0xe4, 0xbc, 0x9a}}},
		{0x1C1, petitionJoin(petitionU64(7), petitionU64(8), petitionWords(10)), Packet{SMSGPetitionSignResults, []byte{1, 0, 7, 1, 0, 8, 0xa0}}},
		{0x1C5, petitionWords(4), Packet{SMSGTurnInPetitionResult, []byte{0x40}}},
		{0x1C2, petitionU64(8), Packet{Opcode: SMSGChat}},
	}
}

func TestPetitionRequestsWireAndMalformed(t *testing.T) {
	for _, f := range petitionRequestFixtures() {
		t.Run(fmt.Sprintf("%x/%d", f.op, len(f.body)), func(t *testing.T) {
			if !IsPetitionClientOpcode(f.op) {
				t.Fatal("missing dispatcher")
			}
			q, err := ParsePetitionRequest(f.op, f.body, petitionTestResolve)
			if err != nil || q.Opcode != f.want.Opcode || !bytes.Equal(q.Body, f.want.Body) {
				t.Fatalf("got %x/%x %v want %x/%x", q.Opcode, q.Body, err, f.want.Opcode, f.want.Body)
			}
			for n := 0; n < len(f.body); n++ {
				if f.op == CMSGTurnInPetition && n == 3 {
					continue
				} // valid short variant
				if _, err := ParsePetitionRequest(f.op, f.body[:n], petitionTestResolve); err == nil {
					t.Fatalf("accepted truncation at %d", n)
				}
			}
			if _, err := ParsePetitionRequest(f.op, append(bytes.Clone(f.body), 0), petitionTestResolve); err == nil {
				t.Fatal("accepted trailing data")
			}
			if _, err := ParsePetitionRequest(f.op, f.body, func(GUID128, PetitionGUIDKind) (uint64, bool) { return 0, false }); err == nil {
				t.Fatal("accepted unknown GUID")
			}
		})
	}
	if IsPetitionClientOpcode(0) || IsPetitionServerOpcode(0) {
		t.Fatal("opcode zero recognized")
	}
}

func TestPetitionResponsesWireAndMalformed(t *testing.T) {
	for _, f := range petitionServerFixtures() {
		t.Run(fmt.Sprintf("%x", f.op), func(t *testing.T) {
			if !IsPetitionServerOpcode(f.op) {
				t.Fatal("missing dispatcher")
			}
			q, err := TranslateLegacyPetition(f.op, f.body, 1, petitionTestGUID, petitionTestAccount, petitionTestName)
			if err != nil || q.Opcode != f.want.Opcode {
				t.Fatalf("%x %v", q.Opcode, err)
			}
			if f.want.Body != nil && !bytes.Equal(q.Body, f.want.Body) {
				t.Fatalf("got  %x\nwant %x", q.Body, f.want.Body)
			}
			if f.op == 0x1C2 && !bytes.Contains(q.Body, []byte("签名者拒绝签署你的公会登记表。")) {
				t.Fatal("decline notification missing")
			}
			for n := 0; n < len(f.body); n++ {
				q, err := TranslateLegacyPetition(f.op, f.body[:n], 1, petitionTestGUID, petitionTestAccount, petitionTestName)
				if err == nil || q.Opcode != 0 || q.Body != nil {
					t.Fatalf("accepted truncation at %d", n)
				}
			}
			if _, err := TranslateLegacyPetition(f.op, append(bytes.Clone(f.body), 0), 1, petitionTestGUID, petitionTestAccount, petitionTestName); err == nil {
				t.Fatal("accepted trailing data")
			}
		})
	}
}

func TestPetitionEmptyListsResultsAndInvalidStrings(t *testing.T) {
	for _, f := range []petitionFixture{
		{0x1BC, petitionJoin(petitionU64(9), []byte{0}), Packet{SMSGPetitionShowList, petitionJoin([]byte{1, 0, 9}, petitionWords(0))}},
		{0x1BF, petitionJoin(petitionU64(7), petitionU64(8), petitionWords(123), []byte{0}), Packet{SMSGPetitionShowSignatures, petitionJoin([]byte{1, 0, 7, 1, 0, 8, 1, 0, 108}, petitionWords(123, 0))}},
	} {
		q, e := TranslateLegacyPetition(f.op, f.body, 1, petitionTestGUID, petitionTestAccount, petitionTestName)
		if e != nil || !bytes.Equal(q.Body, f.want.Body) {
			t.Fatalf("empty list %x %v", q.Body, e)
		}
	}
	for _, op := range []uint16{0x1C1, 0x1C5} {
		for result := uint32(0); result <= 16; result++ {
			b := petitionWords(result)
			if op == 0x1C1 {
				b = petitionJoin(petitionU64(7), petitionU64(8), b)
			}
			q, e := TranslateLegacyPetition(op, b, 1, petitionTestGUID, petitionTestAccount, petitionTestName)
			if result == 16 {
				if e == nil {
					t.Fatal("truncated result")
				}
				continue
			}
			if e != nil || q.Body[len(q.Body)-1] != byte(result<<4) {
				t.Fatal("result mapping")
			}
		}
	}
	if _, e := ParsePetitionRequest(CMSGPetitionRenameGuild, []byte{1, 0, 7, 2, 0}, petitionTestResolve); e == nil {
		t.Fatal("embedded NUL")
	}
	if _, e := TranslateLegacyPetition(0x2C1, petitionJoin(petitionU64(7), bytes.Repeat([]byte{'x'}, 128), []byte{0}), 1, petitionTestGUID, petitionTestAccount, petitionTestName); e == nil {
		t.Fatal("name length overflow")
	}
}

func FuzzPetitionPackets(f *testing.F) {
	for _, p := range petitionRequestFixtures() {
		f.Add(true, p.op, p.body)
	}
	for _, p := range petitionServerFixtures() {
		f.Add(false, p.op, p.body)
	}
	f.Fuzz(func(t *testing.T, client bool, op uint16, b []byte) {
		var q Packet
		var err error
		if client {
			q, err = ParsePetitionRequest(op, b, petitionTestResolve)
		} else {
			q, err = TranslateLegacyPetition(op, b, 1, petitionTestGUID, petitionTestAccount, petitionTestName)
		}
		if err != nil && (q.Opcode != 0 || q.Body != nil) {
			t.Fatal("malformed packet produced output")
		}
	})
}
