package modernworld

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestValidateCUFProfiles(t *testing.T) {
	body, _ := hex.DecodeString("010000001249e000240048000000000000000000000000e4b8bbe9858de7bdae")
	if err := ValidateCUFProfiles(body); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < len(body); n++ {
		if err := ValidateCUFProfiles(body[:n]); err == nil {
			t.Fatalf("accepted truncated size %d", n)
		}
	}
	for _, bad := range [][]byte{append(append([]byte{}, body...), 0), binary.LittleEndian.AppendUint32(nil, ^uint32(0)), make([]byte, MaxCUFProfilesBytes+1)} {
		if err := ValidateCUFProfiles(bad); err == nil {
			t.Fatal("accepted malformed payload")
		}
	}
	if err := ValidateCUFProfiles([]byte{0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
}

func FuzzValidateCUFProfiles(f *testing.F) {
	f.Add(EncodeLoadCUFProfiles(DefaultCUFProfiles()))
	f.Add([]byte{0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, body []byte) { _ = ValidateCUFProfiles(body) })
}

func TestDefaultCUFProfilesMatchesLegacyProxy(t *testing.T) {
	if SMSGLoadCUFProfiles != 0x25BC {
		t.Fatalf("SMSG_LOAD_CUF_PROFILES opcode = %#x, want 0x25BC for build 54261", SMSGLoadCUFProfiles)
	}

	got := EncodeLoadCUFProfiles(DefaultCUFProfiles())
	want, err := hex.DecodeString("010000001249e000240048000000000000000000000000e4b8bbe9858de7bdae")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("default CUF profiles = %x, want legacy proxy payload %x", got, want)
	}
}
