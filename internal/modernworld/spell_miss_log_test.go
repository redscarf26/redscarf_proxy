package modernworld

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestSpellMissLogWire(t *testing.T) {
	for _, tc := range []struct {
		legacy, modern string
		count          int
	}{
		{"3412000001000000000000000001000000020000000000000007", "34120000010001010000000100020700", 1},
		{"34120000010000000000000001020000000200000000000000070000803f0000004003000000000000000b0000404000008040", "341200000100010200000001000207800000803f000000400100030b800000404000008040", 2},
		{"3412000001000000000000000000000000", "3412000001000100000000", 0},
	} {
		body, _ := hex.DecodeString(tc.legacy)
		want, _ := hex.DecodeString(tc.modern)
		log, err := ParseLegacySpellMissLog(body)
		if err != nil || len(log.Entries) != tc.count {
			t.Fatalf("parse: %+v %v", log, err)
		}
		got := EncodeSpellMissLog(log, func(g uint64) GUID128 { return GUID128{Low: g} })
		if !bytes.Equal(got, want) {
			t.Fatalf("wire = %x want %x", got, want)
		}
		for n := 0; n < len(body); n++ {
			if _, err := ParseLegacySpellMissLog(body[:n]); err == nil {
				t.Fatalf("accepted truncation %d", n)
			}
		}
		if _, err := ParseLegacySpellMissLog(append(body, 0)); err == nil {
			t.Fatal("accepted trailing byte")
		}
	}
	body, _ := hex.DecodeString("34120000010000000000000000ffffffff")
	if _, err := ParseLegacySpellMissLog(body); err == nil {
		t.Fatal("accepted oversized count")
	}
}
func FuzzParseLegacySpellMissLog(f *testing.F) {
	seed, _ := hex.DecodeString("3412000001000000000000000001000000020000000000000007")
	f.Add(seed)
	f.Fuzz(func(t *testing.T, b []byte) {
		log, err := ParseLegacySpellMissLog(b)
		if err == nil {
			EncodeSpellMissLog(log, func(g uint64) GUID128 { return GUID128{Low: g} })
		}
	})
}
