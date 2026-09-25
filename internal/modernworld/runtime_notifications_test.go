package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestRuntimeNotificationWireLayouts(t *testing.T) {
	guid := uint64(0x01020304050607) // packed GUID is exactly eight bytes
	packed := EncodeLegacyPackedGUID(guid)
	mapper := func(g uint64) GUID128 { return GUID128{Low: g, High: 1} }
	modern := appendPackedGUID128(nil, guid, 1)
	lockout := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24}
	cases := []struct {
		op    uint16
		input []byte
		out   uint16
		want  []byte
	}{
		{0x152, packed, SMSGBreakTarget, modern},
		{0x319, binary.LittleEndian.AppendUint32(append([]byte(nil), packed...), 123), SMSGMoveSkipTime, binary.LittleEndian.AppendUint32(append([]byte(nil), modern...), 123)},
		{0xfa, []byte{42, 0, 0, 0}, SMSGTriggerCinematic, []byte{42, 0, 0, 0, 0, 0}},
		{0x43e, lockout, SMSGCalendarRaidLockoutAdded, append(append([]byte(nil), lockout[16:]...), lockout[:16]...)},
	}
	for _, tc := range cases {
		p, err := TranslateLegacyRuntimeNotification(tc.op, tc.input, mapper)
		if err != nil || p.Opcode != tc.out || !bytes.Equal(p.Body, tc.want) {
			t.Fatalf("op=%x result=%x err=%v", tc.op, p.Body, err)
		}
		for n := 0; n < len(tc.input); n++ {
			if _, err := TranslateLegacyRuntimeNotification(tc.op, tc.input[:n], mapper); err == nil {
				t.Fatalf("op=%x accepted truncation %d", tc.op, n)
			}
		}
		if _, err := TranslateLegacyRuntimeNotification(tc.op, append(append([]byte(nil), tc.input...), 0), mapper); err == nil {
			t.Fatalf("op=%x accepted trailer", tc.op)
		}
	}
	first := append([]byte("测试\x00"), make([]byte, 8)...)
	first = binary.LittleEndian.AppendUint32(first, 1234)
	first = binary.LittleEndian.AppendUint32(first, 1)
	p, err := TranslateLegacyRuntimeNotification(0x498, first, mapper)
	if err != nil || p.Opcode != SMSGChat || !bytes.Contains(p.Body, []byte("测试")) || !bytes.Contains(p.Body, []byte("#1234")) {
		t.Fatal(p, err)
	}
	for n := 0; n < len(first); n++ {
		if _, err := TranslateLegacyRuntimeNotification(0x498, first[:n], mapper); err == nil {
			t.Fatal("truncated achievement accepted", n)
		}
	}
}

func TestHoverObserverBroadcast(t *testing.T) {
	if !IsLegacyPlayerMovementOpcode(0xf7) {
		t.Fatal("hover not registered")
	}
	// Legacy MovementInfo: GUID, flags, extra, time, x/y/z/o, fall time.
	body := EncodeLegacyPackedGUID(42)
	body = binary.LittleEndian.AppendUint32(body, legacyMoveHover)
	body = binary.LittleEndian.AppendUint16(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 123)
	body = append(body, make([]byte, 20)...)
	guid, move, err := ParseLegacyPlayerMovementForOpcode(0xf7, body)
	if err != nil || guid != 42 || move.MoveFlags != legacyMoveHover || move.MoveTime != 123 {
		t.Fatal(guid, move, err)
	}
	if _, _, err = ParseLegacyPlayerMovementForOpcode(0xf7, append(body, 0)); err == nil {
		t.Fatal("hover trailer accepted")
	}
}
