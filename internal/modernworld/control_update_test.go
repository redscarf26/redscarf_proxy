package modernworld

import (
	"strings"
	"testing"
)

func TestControlUpdateTranslation(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0xf130000001000043)
	legacy = append(legacy, 1)
	guid, hasControl, err := ParseLegacyControlUpdate(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if guid != 0xf130000001000043 || !hasControl {
		t.Fatalf("guid=%x control=%v", guid, hasControl)
	}
	modernGUID := GUID128{Low: 0x43, High: 0x1234}
	body := EncodeControlUpdate(modernGUID, true)
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	if low != modernGUID.Low || high != modernGUID.High || consumed+1 != len(body) || body[consumed] != 0x80 {
		t.Fatalf("modern control body=%x", body)
	}
	if got := EncodeMoveSetActiveMover(modernGUID); string(got) != string(body[:consumed]) {
		t.Fatalf("active mover body=%x want=%x", got, body[:consumed])
	}
	parsed, err := ParseSetActiveMover(body[:consumed])
	if err != nil || parsed != modernGUID {
		t.Fatalf("parsed active mover=%#v err=%v", parsed, err)
	}
}

func TestControlUpdateRejectsMalformed(t *testing.T) {
	for _, test := range []struct {
		body []byte
		want string
	}{
		{body: []byte{1}, want: "controlled GUID"},
		{body: append(appendLegacyPackedGUID(nil, 1), 2), want: "invalid control"},
		{body: append(appendLegacyPackedGUID(nil, 1), 1, 0), want: "trailing"},
	} {
		_, _, err := ParseLegacyControlUpdate(test.body)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("err=%v want=%q", err, test.want)
		}
	}
	if mover, err := ParseSetActiveMover([]byte{0, 0}); err != nil || mover != (GUID128{}) {
		t.Fatalf("empty active mover=%#v err=%v", mover, err)
	}
}
