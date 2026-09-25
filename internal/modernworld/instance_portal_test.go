package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestHellfirePortalQueryWire(t *testing.T) {
	// 12340 template structure from the captured 184131 portal: type31,
	// display7148, map543, difficulty0. Strings make the Data offset variable.
	for _, tc := range []struct{ entry, mapID uint32 }{{184127, 540}, {184130, 542}, {184131, 543}, {184175, 542}, {184177, 540}, {184179, 543}} {
		legacy := pveWords(tc.entry, 31, 7148)
		legacy = appendCString(legacy, "Doodad_InstancePortal_PurpleDifficulty03")
		legacy = append(legacy, make([]byte, 6)...)
		legacy = append(legacy, pveWords(tc.mapID)...)
		legacy = append(legacy, make([]byte, 23*4)...)
		legacy = append(legacy, pveWords(0x3fea3d71, 0, 0, 0, 0, 0, 0)...)
		before := append([]byte(nil), legacy...)
		body, err := TranslateGameObjectQueryResponse(legacy, GUID128{})
		if err != nil {
			t.Fatal(err)
		}
		r := movementReader{data: body}
		gotEntry, _ := r.u32()
		_, _ = r.guid128()
		valid, _ := r.bit()
		r.align()
		size, _ := r.u32()
		if gotEntry != tc.entry || !valid || int(size) != r.remaining() {
			t.Fatalf("bad response envelope entry=%d size=%d remaining=%d", gotEntry, size, r.remaining())
		}
		typ, _ := r.u32()
		display, _ := r.u32()
		for range 7 {
			if _, err := r.cstring(); err != nil {
				t.Fatal(err)
			}
		}
		want := [35]uint32{0: 1, 1: 214, 2: 215, 5: 7149, 7: 2}
		for i, w := range want {
			v, err := r.u32()
			if err != nil || v != w {
				t.Fatalf("entry %d data[%d]=%d want %d err=%v", tc.entry, i, v, w, err)
			}
		}
		scale, _ := r.u32()
		if typ != 31 || display != 7148 || scale != 0x3fea3d71 || !bytes.Equal(legacy, before) {
			t.Fatal("portal identity, scale or input mutated")
		}
	}
}

func TestPortalMappingDoesNotRewriteOtherTemplates(t *testing.T) {
	for _, tc := range []struct {
		entry, typ, display uint32
		mapID               int32
	}{{184131, 5, 7148, 543}, {184131, 31, 8196, 543}, {184131, 31, 7148, 999}, {196391, 31, 8196, 632}, {202318, 31, 9041, 631}} {
		s := GameObjectQueryStats{Type: tc.typ, DisplayID: tc.display, Data: [24]int32{tc.mapID, 0, 99}}
		before := s.Data
		translateInstancePortalStats(tc.entry, &s)
		if s.Data != before {
			t.Fatalf("rewrote unrelated/custom template %d", tc.entry)
		}
	}
}

func TestOnlyPairedHellfireSkullsAreSuppressed(t *testing.T) {
	for _, tc := range []struct {
		entry, display uint32
		typ            byte
		want           bool
	}{{184128, 7149, 31, true}, {184129, 7149, 31, true}, {184132, 7149, 31, true}, {184176, 7149, 31, true}, {184178, 7149, 31, true}, {184180, 7149, 31, true}, {184131, 7148, 31, false}, {188177, 7149, 31, false}, {201756, 8197, 31, false}, {184132, 7149, 5, false}, {184132, 7148, 31, false}} {
		update := LegacyObjectUpdate{ObjectType: 5, Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{legacyObjectEntry: tc.entry, legacyGameObjectDisplayID: tc.display, legacyGameObjectBytes1: uint32(tc.typ) << 8}}}
		if got := IsRedundantInstancePortalSkull(update); got != tc.want {
			t.Fatalf("entry=%d type=%d display=%d skip=%v", tc.entry, tc.typ, tc.display, got)
		}
		update.ObjectType = 3
		if IsRedundantInstancePortalSkull(update) {
			t.Fatal("filtered a creature")
		}
	}
}

func TestProxyCacheRevisionInvalidatesOldTemplates(t *testing.T) {
	for _, v := range []uint32{0, 1, 12340} {
		encoded := EncodeLegacyCacheVersion(v)
		if len(encoded) != 4 || binary.LittleEndian.Uint32(encoded) == v {
			t.Fatal("old template cache would be reused")
		}
		if bytes.Equal(encoded, EncodeLegacyCacheVersion(v+1)) {
			t.Fatal("upstream revision lost")
		}
	}
}
