package modernworld

import (
	"encoding/hex"
	"math"
	"testing"
	"time"
)

func skybreakerFields(dynamic, bytes1 uint32) map[int]uint32 {
	return map[int]uint32{
		legacyObjectEntry:         201580,
		legacyGameObjectDynamic:   dynamic,
		legacyGameObjectBytes1:    bytes1,
		legacyGameObjectLevel:     78697,
		legacyGameObjectDisplayID: 9150,
	}
}

func TestTransportResyncIsOffByDefault(t *testing.T) {
	var sync TransportPathSync
	start := time.Now()
	sync.Observe(0x1fc0000000000015, skybreakerFields(0x10640000, 0xff000f00), start)
	if got := sync.Due(start.Add(time.Minute)); got != nil {
		t.Fatalf("resync without REDSCARF_TRANSPORT_RESYNC = %v, want none", got)
	}
}

func TestTransportResyncFollowsRealmTimer(t *testing.T) {
	// proxy.log 22:03:10.146 reported progress 0x1064 with state 0 and
	// 22:03:49.452 reported 0x90e1, so replaying the realm's own timer across
	// those 39.306 s must land on its next value rather than drift.
	var sync TransportPathSync
	start := time.Now()
	sync.Observe(0x1fc0000000000015, skybreakerFields(0x10640000, 0xff000f00), start)
	resyncs := sync.due(start.Add(39306*time.Millisecond), time.Second)
	if len(resyncs) != 1 {
		t.Fatalf("resyncs = %d, want 1", len(resyncs))
	}
	if resyncs[0].GUID != 0x1fc0000000000015 {
		t.Fatalf("resync GUID = 0x%x", resyncs[0].GUID)
	}
	progress := resyncs[0].Fields[legacyGameObjectDynamic] >> 16
	const realm = 0x90e1
	if diff := int(realm) - int(progress); diff < 0 || diff > 400 {
		t.Fatalf("replayed progress 0x%x, realm reported 0x%x", progress, realm)
	}
	if state := resyncs[0].Fields[legacyGameObjectBytes1]; state != 0xff000f00 {
		t.Fatalf("replayed state = 0x%x, want the realm's 0xff000f00", state)
	}
}

func TestTransportResyncRateLimitsAndStopsWhenParked(t *testing.T) {
	var sync TransportPathSync
	start := time.Now()
	guid := uint64(0x1fc0000000000015)
	sync.Observe(guid, skybreakerFields(0x10640000, 0xff000f00), start)
	if got := sync.due(start.Add(400*time.Millisecond), time.Second); got != nil {
		t.Fatalf("resync before the interval elapsed = %v", got)
	}
	first := sync.due(start.Add(1200*time.Millisecond), time.Second)
	if len(first) != 1 {
		t.Fatalf("first resync count = %d, want 1", len(first))
	}
	if got := sync.due(start.Add(1600*time.Millisecond), time.Second); got != nil {
		t.Fatalf("second resync inside the interval = %v", got)
	}
	// A parked hull must be left on the realm's last progress: the encounter
	// stops both gunships mid-path and the client has to hold there.
	sync.Observe(guid, skybreakerFields(0x90e10000, 0xff000f01), start.Add(2*time.Second))
	if got := sync.due(start.Add(10*time.Second), time.Second); got != nil {
		t.Fatalf("resync for a parked hull = %v", got)
	}
}

func TestTransportResyncIgnoresMapPlatformsAndMissingPeriod(t *testing.T) {
	var sync TransportPathSync
	start := time.Now()
	// 0xf120 platforms carry a DB2 period and already animate correctly.
	sync.Observe(0xf120000000000003, skybreakerFields(0x10640000, 0xff000f00), start)
	// Without GAMEOBJECT_LEVEL there is no path time to extrapolate against.
	noPeriod := skybreakerFields(0x10640000, 0xff000f00)
	delete(noPeriod, legacyGameObjectLevel)
	sync.Observe(0x1fc0000000000016, noPeriod, start)
	if got := sync.due(start.Add(5*time.Second), time.Second); got != nil {
		t.Fatalf("resyncs = %v, want none", got)
	}
}

func TestTransportResyncEncodesLikeARealmProgressUpdate(t *testing.T) {
	// proxy.log 22:03:10.146 sent this exact body for the realm's own progress
	// word, so a replay must be indistinguishable from it on the wire.
	const want = "010000007702001d0000000000b040051813000000010100005004016410780010000fff00000000"
	legacyGUID := uint64(0x1fc0000000000015)
	merged := skybreakerFields(0x10640000, 0xff000f00)
	merged[legacyObjectScale] = math.Float32bits(1)
	body, _, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: legacyGUID, ObjectType: 5,
		Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{
			legacyGameObjectDynamic: 0x10640000, legacyGameObjectBytes1: 0xff000f00,
		}},
	}, ValuesUpdateOptions{
		MapID: 631, ObjectType: 5, GUID: ModernGUIDForLegacy(legacyGUID, 631), Fields: merged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(body); got != want {
		t.Fatalf("replayed body\ngot  %s\nwant %s", got, want)
	}
}

func TestParseTransportResyncInterval(t *testing.T) {
	for raw, want := range map[string]time.Duration{
		"":      0,
		"0s":    0,
		"-1s":   0,
		"junk":  0,
		"off":   0,
		"0":     0,
		"false": 0,
		"1s":    time.Second,
		"500ms": 500 * time.Millisecond,
	} {
		if got := parseTransportResyncInterval(raw); got != want {
			t.Fatalf("parseTransportResyncInterval(%q) = %v, want %v", raw, got, want)
		}
	}
}
