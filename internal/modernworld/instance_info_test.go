package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestInstanceInfoTranslation(t *testing.T) {
	if err := ParseRequestRaidInfo(nil); err != nil {
		t.Fatal(err)
	}
	legacy := binary.LittleEndian.AppendUint32(nil, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 533)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x1234)
	legacy = append(legacy, 1, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 3600)
	locks, err := ParseLegacyInstanceInfo(legacy)
	if err != nil || len(locks) != 1 || !locks[0].Locked || !locks[0].Extended || locks[0].TimeRemaining != 3600 {
		t.Fatalf("locks=%#v err=%v", locks, err)
	}
	body := EncodeInstanceInfo(locks)
	if len(body) != 4+4+4+8+4+4+1 || binary.LittleEndian.Uint32(body[4:8]) != 533 || body[len(body)-1]&0xc0 != 0xc0 {
		t.Fatalf("modern instance info=%x", body)
	}
}

func TestInstanceOwnershipTranslation(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 1)
	translated, err := TranslateInstanceOwnership(body)
	if err != nil || !bytes.Equal(translated, body) {
		t.Fatalf("translated=%x err=%v", translated, err)
	}
	if _, err := TranslateInstanceOwnership(body[:3]); err == nil {
		t.Fatal("expected malformed ownership error")
	}
}

func TestInstanceResetTranslation(t *testing.T) {
	if err := ParseResetInstances(nil); err != nil {
		t.Fatal(err)
	}
	if err := ParseResetInstances([]byte{0}); err == nil {
		t.Fatal("expected reset-instances body error")
	}

	legacyReset := binary.LittleEndian.AppendUint32(nil, 36)
	mapID, err := ParseLegacyInstanceReset(legacyReset)
	if err != nil || mapID != 36 {
		t.Fatalf("map=%d err=%v", mapID, err)
	}
	if body := EncodeInstanceReset(mapID); !bytes.Equal(body, legacyReset) {
		t.Fatalf("modern instance reset=%x", body)
	}

	legacyFailure := binary.LittleEndian.AppendUint32(nil, 2)
	legacyFailure = binary.LittleEndian.AppendUint32(legacyFailure, 36)
	failure, err := ParseLegacyInstanceResetFailed(legacyFailure)
	if err != nil || failure.Reason != 2 || failure.MapID != 36 {
		t.Fatalf("failure=%#v err=%v", failure, err)
	}
	body := EncodeInstanceResetFailed(failure)
	if len(body) != 5 || binary.LittleEndian.Uint32(body[:4]) != 36 || body[4] != 0x80 {
		t.Fatalf("modern instance reset failed=%x", body)
	}

	legacyNotify := binary.LittleEndian.AppendUint32(nil, 36)
	if notifyMapID, err := ParseLegacyResetFailedNotify(legacyNotify); err != nil || notifyMapID != 36 {
		t.Fatalf("notify map=%d err=%v", notifyMapID, err)
	}
}

func TestMalformedInstanceResetMessages(t *testing.T) {
	if _, err := ParseLegacyInstanceReset([]byte{0, 0, 0}); err == nil {
		t.Fatal("expected malformed instance-reset error")
	}
	invalidReason := binary.LittleEndian.AppendUint32(nil, 4)
	invalidReason = binary.LittleEndian.AppendUint32(invalidReason, 36)
	if _, err := ParseLegacyInstanceResetFailed(invalidReason); err == nil {
		t.Fatal("expected out-of-range reset reason error")
	}
	if _, err := ParseLegacyInstanceResetFailed(invalidReason[:7]); err == nil {
		t.Fatal("expected malformed instance-reset-failed error")
	}
	if _, err := ParseLegacyResetFailedNotify([]byte{0, 0, 0}); err == nil {
		t.Fatal("expected malformed reset-failed-notify error")
	}
}
