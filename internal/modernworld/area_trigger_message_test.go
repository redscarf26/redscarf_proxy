package modernworld

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestTranslateLegacyAreaTriggerMessage(t *testing.T) {
	for _, message := range []string{"", "你必须在一个团队中才能进入这个副本。", "你的等级必须达到|CFF9900CC70|R才能进入。", strings.Repeat("a", 4095)} {
		legacy := binary.LittleEndian.AppendUint32(nil, uint32(len(message)+1))
		legacy = append(legacy, []byte(message)...)
		legacy = append(legacy, 0)
		got, err := TranslateLegacyAreaTriggerMessage(legacy)
		if err != nil {
			t.Fatal(err)
		}
		// Independently check the MSB-first 12-bit UTF-8 byte length and no NUL.
		want := append([]byte{byte(len(message) >> 4), byte(len(message) << 4)}, []byte(message)...)
		if !bytes.Equal(got, want) {
			t.Fatalf("notification mismatch: got %x want %x", got, want)
		}
	}
}

func TestTranslateLegacyAreaTriggerMessageRejectsMalformed(t *testing.T) {
	cases := [][]byte{nil, {1, 0, 0}, {0, 0, 0, 0}, {2, 0, 0, 0, 0}, {1, 0, 0, 0, 'x'}, {3, 0, 0, 0, 'x', 0, 0}, {255, 255, 255, 255, 0}}
	tooLong := binary.LittleEndian.AppendUint32(nil, 4097)
	tooLong = append(tooLong, []byte(strings.Repeat("a", 4096))...)
	cases = append(cases, append(tooLong, 0))
	for _, body := range cases {
		if _, err := TranslateLegacyAreaTriggerMessage(body); err == nil {
			t.Fatalf("accepted malformed packet of %d bytes", len(body))
		}
	}
}
