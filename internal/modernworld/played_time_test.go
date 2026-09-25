package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestPlayedTimeTranslation(t *testing.T) {
	trigger, err := ParseRequestPlayedTime([]byte{0x80})
	if err != nil || !trigger {
		t.Fatalf("trigger=%v err=%v", trigger, err)
	}
	if _, err := ParseRequestPlayedTime(nil); err == nil {
		t.Fatal("empty request unexpectedly parsed")
	}
	legacy := binary.LittleEndian.AppendUint32(nil, 123)
	legacy = binary.LittleEndian.AppendUint32(legacy, 45)
	legacy = append(legacy, 1)
	modern, err := EncodePlayedTime(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(modern) != 9 || binary.LittleEndian.Uint32(modern) != 123 || binary.LittleEndian.Uint32(modern[4:]) != 45 || modern[8] != 0x80 {
		t.Fatalf("modern played time = %x", modern)
	}
	if _, err := EncodePlayedTime(legacy[:8]); err == nil {
		t.Fatal("truncated played time unexpectedly translated")
	}
}
