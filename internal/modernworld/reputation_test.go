package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestParseFactionAtWar(t *testing.T) {
	index, err := ParseFactionAtWar([]byte{7})
	if err != nil || index != 7 {
		t.Fatalf("parse faction-at-war index=%d err=%v", index, err)
	}
	if _, err := ParseFactionAtWar(nil); err == nil {
		t.Fatal("empty faction-at-war body must error")
	}
	if _, err := ParseFactionAtWar([]byte{1, 2}); err == nil {
		t.Fatal("two-byte faction-at-war body must error")
	}
}

func TestEncodeLegacyFactionAtWar(t *testing.T) {
	if got := EncodeLegacyFactionAtWar(7, true); !bytes.Equal(got, []byte{7, 0, 0, 0, 1}) {
		t.Fatalf("at-war body=%x", got)
	}
	if got := EncodeLegacyFactionAtWar(7, false); !bytes.Equal(got, []byte{7, 0, 0, 0, 0}) {
		t.Fatalf("not-at-war body=%x", got)
	}
}

func TestParseFactionInactive(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 0x12345678)
	body = append(body, 0x80) // inactive state bit set in the final byte
	change, err := ParseFactionInactive(body)
	if err != nil {
		t.Fatalf("parse faction-inactive: %v", err)
	}
	if change.Index != 0x12345678 || !change.Inactive {
		t.Fatalf("faction-inactive change=%+v", change)
	}

	clear := binary.LittleEndian.AppendUint32(nil, 0x12345678)
	clear = append(clear, 0x00) // inactive state bit clear
	change, err = ParseFactionInactive(clear)
	if err != nil {
		t.Fatalf("parse cleared faction-inactive: %v", err)
	}
	if change.Inactive {
		t.Fatalf("cleared faction-inactive state must be false")
	}

	if _, err := ParseFactionInactive(binary.LittleEndian.AppendUint32(nil, 5)); err == nil {
		t.Fatal("faction-inactive without a state bit must error")
	}
	if _, err := ParseFactionInactive(append(body, 0)); err == nil {
		t.Fatal("faction-inactive with trailing bytes must error")
	}
}

func TestEncodeLegacyFactionInactive(t *testing.T) {
	if got := EncodeLegacyFactionInactive(0x12345678, true); !bytes.Equal(got, []byte{0x78, 0x56, 0x34, 0x12, 1}) {
		t.Fatalf("inactive body=%x", got)
	}
	if got := EncodeLegacyFactionInactive(0x12345678, false); !bytes.Equal(got, []byte{0x78, 0x56, 0x34, 0x12, 0}) {
		t.Fatalf("active body=%x", got)
	}
}

func TestParseWatchedFaction(t *testing.T) {
	index, err := ParseWatchedFaction(binary.LittleEndian.AppendUint32(nil, 99))
	if err != nil || index != 99 {
		t.Fatalf("parse watched-faction index=%d err=%v", index, err)
	}
	if _, err := ParseWatchedFaction([]byte{1, 2, 3}); err == nil {
		t.Fatal("short watched-faction body must error")
	}
	if _, err := ParseWatchedFaction([]byte{1, 2, 3, 4, 5}); err == nil {
		t.Fatal("long watched-faction body must error")
	}
	if got := EncodeLegacyWatchedFaction(99); !bytes.Equal(got, binary.LittleEndian.AppendUint32(nil, 99)) {
		t.Fatalf("watched-faction body=%x", got)
	}
}

func TestFactionVisibleRoundTrip(t *testing.T) {
	index, err := ParseLegacyFactionVisible(binary.LittleEndian.AppendUint32(nil, 17))
	if err != nil || index != 17 {
		t.Fatalf("parse faction-visible index=%d err=%v", index, err)
	}
	if _, err := ParseLegacyFactionVisible([]byte{1, 2, 3, 4, 5}); err == nil {
		t.Fatal("long faction-visible body must error")
	}
	if got := EncodeFactionVisible(17); !bytes.Equal(got, binary.LittleEndian.AppendUint32(nil, 17)) {
		t.Fatalf("faction-visible body=%x", got)
	}
}
