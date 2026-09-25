package modernworld

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestServerTimeOffsetResponse(t *testing.T) {
	if err := ParseServerTimeOffsetRequest(nil); err != nil {
		t.Fatal(err)
	}
	if err := ParseServerTimeOffsetRequest([]byte{0}); err == nil {
		t.Fatal("non-empty server-time request accepted")
	}
	body := EncodeServerTimeOffset(time.Unix(1_700_000_000, 0))
	if len(body) != 4 || binary.LittleEndian.Uint32(body) != 1_700_000_000 {
		t.Fatalf("response %x", body)
	}
}

func TestDungeonDifficultyTranslation(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	difficulty, err := ParseLegacyDungeonDifficulty(legacy)
	if err != nil || difficulty != 2 {
		t.Fatalf("difficulty=%d err=%v", difficulty, err)
	}
	if binary.LittleEndian.Uint32(EncodeDungeonDifficulty(difficulty)) != 2 {
		t.Fatal("modern dungeon difficulty mismatch")
	}
}

func TestFeatureSystemStatus54261Shape(t *testing.T) {
	body := EncodeFeatureSystemStatus()
	// 73 fixed bytes + 6 feature-bit bytes + 1 quick-join bit byte +
	// 22 floats + 1 squelch bit byte + two empty packed GUIDs.
	if len(body) != 173 {
		t.Fatalf("feature-system-status has %d bytes, want 173", len(body))
	}
	if body[0] != 2 || binary.LittleEndian.Uint32(body[1:5]) != 1 || binary.LittleEndian.Uint32(body[69:73]) != 10 {
		t.Fatalf("unexpected feature-system-status header: %x", body[:73])
	}
}

func TestMOTDEncoding(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 2)
	legacy = append(legacy, "Welcome"...)
	legacy = append(legacy, 0)
	legacy = append(legacy, "Rabbit"...)
	legacy = append(legacy, 0)
	body, err := EncodeMOTD(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 1+1+7+1+6 || body[0] != 0x20 || body[1] != 0x0e || string(body[2:9]) != "Welcome" || body[9] != 0x0c || string(body[10:]) != "Rabbit" {
		t.Fatalf("unexpected MOTD body: %x", body)
	}
}

func TestTutorialActionRoundTrip(t *testing.T) {
	want := TutorialAction{Action: TutorialUpdate, Bit: 0x12345678}
	got, err := ParseTutorialAction(EncodeTutorialAction(want))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("tutorial action mismatch: got=%#v want=%#v", got, want)
	}
	for _, action := range []byte{TutorialClear, TutorialReset} {
		got, err := ParseTutorialAction(EncodeTutorialAction(TutorialAction{Action: action}))
		if err != nil || got.Action != action {
			t.Fatalf("tutorial action %d: got=%#v err=%v", action, got, err)
		}
	}
}
