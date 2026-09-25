package opcodes

import "testing"

func TestGeneratedRegistries(t *testing.T) {
	if len(Modern54261) != 273 || len(Legacy12340) != 313 {
		t.Fatalf("unexpected registry sizes: modern=%d legacy=%d", len(Modern54261), len(Legacy12340))
	}
	if got := Modern54261[0x35e9]; got != "CMSG_ENUM_CHARACTERS" {
		t.Fatalf("modern character enum name = %q", got)
	}
	if got := Legacy12340[0x003b]; got != "SMSG_ENUM_CHARACTERS_RESULT" {
		t.Fatalf("legacy character enum name = %q", got)
	}
}
