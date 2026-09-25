package modernworld

import (
	"reflect"
	"testing"
)

func readBatchActiveMask(t *testing.T, body []byte) (*movementReader, []uint32) {
	t.Helper()
	r := &movementReader{data: body}
	lo, err := r.u32()
	if err != nil {
		t.Fatal(err)
	}
	hi, err := r.bits(16)
	if err != nil {
		t.Fatal(err)
	}
	blocks := make([]uint32, 48)
	for i := range blocks {
		if i < 32 && lo&(1<<i) != 0 || i >= 32 && hi&(1<<(i-32)) != 0 {
			blocks[i], err = r.bits(32)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	r.align()
	return r, blocks
}

func TestValuesExplorationRestAndClear(t *testing.T) {
	for _, v := range []uint32{12345, 0} {
		fields := map[int]uint32{legacyPlayerExplored1: 99, legacyPlayerExplored1 + 1: v, legacyPlayerExplored1 + 127: 77, legacyPlayerRestXP: v, legacyPlayerBytes2: 2 << 24}
		changed := map[int]uint32{legacyPlayerExplored1 + 1: v, legacyPlayerExplored1 + 127: 77, legacyPlayerRestXP: v, legacyPlayerBytes2: 2 << 24}
		body := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: fields}, changed, 0)
		r, mask := readBatchActiveMask(t, body)
		for _, bit := range []int{298, 299, 362, 539, 540} {
			if mask[bit/32]&(1<<(bit%32)) == 0 {
				t.Fatalf("missing bit %d", bit)
			}
		}
		first, _ := r.u64()
		last, _ := r.u64()
		if first != uint64(v)<<32|99 || last != uint64(77)<<32 {
			t.Fatalf("merged halves %x %x", first, last)
		}
		restMask, _ := r.bits(3)
		r.align()
		xp, _ := r.u32()
		state, _ := r.u8()
		if restMask != 7 || xp != v || state != 2 || r.remaining() != 0 {
			t.Fatal(restMask, xp, state, r.remaining())
		}
	}
}

func TestValuesTitlesPreserveOtherHalves(t *testing.T) {
	fields := map[int]uint32{626: 11, 627: 22, 628: 33, 629: 44, 630: 55, 631: 66}
	for _, value := range []uint32{77, 0} {
		fields[627] = value
		body := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: fields}, map[int]uint32{627: value}, 0)
		// The dynamic mask is part of the bit stream following block masks.
		r := movementReader{data: body}
		lo, _ := r.u32()
		hi, _ := r.bits(16)
		block, _ := r.bits(32)
		count, _ := r.bits(32)
		elements, _ := r.bits(3)
		r.align()
		if lo != 1 || hi != 0 || block != 9 || count != 3 || elements != 7 {
			t.Fatalf("title masks %x/%x %x %d %x", lo, hi, block, count, elements)
		}
		for _, want := range []uint64{uint64(value)<<32 | 11, 44<<32 | 33, 66<<32 | 55} {
			got, err := r.u64()
			if err != nil || got != want {
				t.Fatal(got, want, err)
			}
		}
		if r.remaining() != 0 {
			t.Fatal("title trailing bytes")
		}
	}
}

func TestValuesUnitArraysUseClientElementOrder(t *testing.T) {
	fields := map[int]uint32{legacyUnitBytes0: 8 << 8, 40: 10, 47: 20, 25: 30, 33: 40,
		84: 101, 85: 102, 89: 201, 90: 202, 94: 301, 95: 302,
		99: 401, 100: 402, 131: 501, 132: 502, 138: 601, 139: 602, 106: 701, 107: 702, 113: 801, 114: 802}
	changed := cloneFields(fields)
	delete(changed, legacyUnitBytes0)
	body := encodeUnitValuesDelta(legacyActivePlayerValues{fields: fields}, changed, 4, 0)
	r := movementReader{data: body}
	blocks, _ := r.bits(8)
	for i := 0; i < 8; i++ {
		if blocks&(1<<i) != 0 {
			if _, err := r.bits(32); err != nil {
				t.Fatal(err)
			}
		}
	}
	r.align()
	// Independent expected traversal from WPP343: power slot; stat index;
	// resistance/cost index; positive/negative resistance-buff index.
	for _, want := range []uint32{10, 20, 30, 40, 101, 201, 301, 102, 202, 302, 401, 501, 601, 402, 502, 602, 701, 801, 702, 802} {
		got, err := r.u32()
		if err != nil || got != want {
			t.Fatalf("got %d want %d (%v)", got, want, err)
		}
	}
	if r.remaining() != 0 {
		t.Fatal("unit trailing bytes")
	}
}

func TestValuesSpellAndBuybackArrayOrder(t *testing.T) {
	fields := map[int]uint32{1032: 1, 1033: 2, 1171: 3, 1172: 4, 1185: 5, 1186: 6, 1201: 7, 1202: 8, 1213: 9, 1214: 10}
	body := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: fields}, fields, 0)
	r, _ := readBatchActiveMask(t, body)
	for _, want := range []uint32{1, 3, 5, 2, 4, 6} {
		got, _ := r.u32()
		if got != want {
			t.Fatal(got, want)
		}
	}
	for _, pair := range [][2]uint64{{7, 9}, {8, 10}} {
		p, _ := r.u32()
		ts, _ := r.u64()
		if uint64(p) != pair[0] || ts != pair[1] {
			t.Fatal(p, ts, pair)
		}
	}
	if r.remaining() != 0 {
		t.Fatal("active trailing bytes")
	}
}

func TestValuesAuditDoesNotCountUnknownInput(t *testing.T) {
	fields := map[int]uint32{legacyUnitHealth: 12, 9999: 1}
	u := LegacyObjectUpdate{Type: LegacyUpdateValues, GUID: 1, Values: LegacyUpdateValuesBlock{Fields: fields}}
	o := ValuesUpdateOptions{ObjectType: 3, GUID: GUID128{Low: 1}, Fields: fields}
	_, count, err := EncodeValuesUpdate(u, o)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	a := AuditValuesFields(u, o)
	if !reflect.DeepEqual(a.Emitted, []int{24}) || !reflect.DeepEqual(a.Deferred, []int{9999}) {
		t.Fatal(a)
	}
}

func TestDynamicObjectValuesClearRadiusAndCaster(t *testing.T) {
	fields := map[int]uint32{6: 0, 7: 0, 9: 43265, 10: 0, 11: 123}
	body := encodeDynamicObjectValuesDelta(legacyActivePlayerValues{fields: fields}, fields, 571)
	r := movementReader{data: body}
	mask, _ := r.bits(7)
	r.align()
	caster, err := r.guid128()
	if mask != 0x7b || err != nil || caster != (GUID128{}) {
		t.Fatal(mask, caster, err)
	}
	visual, _ := r.u32()
	spell, _ := r.u32()
	radius, _ := r.u32()
	stamp, _ := r.u32()
	if visual != KnownSpellVisual(43265) || spell != 43265 || radius != 0 || stamp != 123 || r.remaining() != 0 {
		t.Fatal(visual, spell, radius, stamp)
	}
}

func BenchmarkValuesSkillBatch(b *testing.B) {
	fields := make(map[int]uint32)
	for i := 0; i < 384; i++ {
		fields[636+i] = uint32(i + 1)
	}
	u := LegacyObjectUpdate{Type: LegacyUpdateValues, GUID: 1, Values: LegacyUpdateValuesBlock{Fields: fields}}
	o := ValuesUpdateOptions{ObjectType: 4, Active: true, GUID: GUID128{Low: 1}, Fields: fields}
	for i := 0; i < b.N; i++ {
		_, _, _ = EncodeValuesUpdate(u, o)
	}
}
