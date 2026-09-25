package realm

import (
	"encoding/json"
	"testing"

	"redscarf/internal/legacyauth"
)

func TestEncodeRealmList(t *testing.T) {
	realms := []legacyauth.Realm{{
		ID:             1,
		Type:           0,
		Name:           "AzerothCore",
		Flags:          0,
		Population:     0.5,
		CharacterCount: 2,
		Timezone:       1,
	}}
	blob, err := EncodeList(realms, SubRegion, 54261)
	if err != nil {
		t.Fatal(err)
	}
	name, payload, err := InflateJSON(blob)
	if err != nil {
		t.Fatal(err)
	}
	if name != "JSONRealmListUpdates" {
		t.Fatalf("name %q", name)
	}
	var decoded realmListUpdates
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Updates) != 1 {
		t.Fatalf("updates=%d JSON=%s", len(decoded.Updates), payload)
	}
	entry := decoded.Updates[0].Update
	if entry.Address != 0x01010001 || entry.Name != "AzerothCore" || entry.Version.Build != 54261 || entry.Version.Major != 3 || entry.Version.Minor != 4 || entry.Version.Revision != 3 {
		t.Fatalf("unexpected realm entry: %#v", entry)
	}
}

func TestEncodeCharacterCounts(t *testing.T) {
	blob, err := EncodeCharacterCounts([]legacyauth.Realm{{ID: 7, CharacterCount: 4}})
	if err != nil {
		t.Fatal(err)
	}
	name, payload, err := InflateJSON(blob)
	if err != nil {
		t.Fatal(err)
	}
	if name != "JSONRealmCharacterCountList" {
		t.Fatalf("name %q", name)
	}
	var decoded characterCountList
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Counts) != 1 || decoded.Counts[0].Address != 0x01010007 || decoded.Counts[0].Count != 4 {
		t.Fatalf("unexpected counts: %#v", decoded)
	}
}

func TestEncodeServerAddresses(t *testing.T) {
	blob, err := EncodeServerAddresses("127.0.0.1", 7002)
	if err != nil {
		t.Fatal(err)
	}
	name, payload, err := InflateJSON(blob)
	if err != nil {
		t.Fatal(err)
	}
	if name != "JSONRealmListServerIPAddresses" {
		t.Fatalf("name %q", name)
	}
	var decoded serverAddresses
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Families) != 1 || len(decoded.Families[0].Addresses) != 1 || decoded.Families[0].Addresses[0].IP != "127.0.0.1" || decoded.Families[0].Addresses[0].Port != 7002 {
		t.Fatalf("unexpected server addresses: %#v", decoded)
	}
}
