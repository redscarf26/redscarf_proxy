package modernworld

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"testing"
	"time"
)

func TestEncodeNonUnitCreates(t *testing.T) {
	const ownerGUID = 0x42
	baseFields := map[int]uint32{
		legacyObjectEntry: 123,
		legacyObjectScale: math.Float32bits(1),
	}
	with := func(extra map[int]uint32) map[int]uint32 {
		fields := make(map[int]uint32, len(baseFields)+len(extra))
		for key, value := range baseFields {
			fields[key] = value
		}
		for key, value := range extra {
			fields[key] = value
		}
		return fields
	}
	tests := []struct {
		name          string
		guid          uint64
		legacyType    byte
		modernType    byte
		fields        map[int]uint32
		encode        func(LegacyObjectUpdate, ActivePlayerCreateOptions) ([]byte, error)
		movementBits  [3]byte
		movementBytes int
		valuesBytes   int
		ownerFlags    byte
	}{
		{
			name:       "owned item",
			guid:       0x4000000000000043,
			legacyType: 1,
			modernType: 1,
			fields: with(map[int]uint32{
				legacyItemOwner: ownerGUID, legacyItemStackCount: 2,
				legacyItemDurability: 50, legacyItemMaxDurability: 60,
			}),
			encode: EncodeItemCreate, movementBits: [3]byte{0, 0, 0},
			movementBytes: 7, valuesBytes: 1 + 12 + 263, ownerFlags: 0x03,
		},
		{
			name:       "owned container",
			guid:       0x4000000000000044,
			legacyType: 2,
			modernType: 2,
			fields: with(map[int]uint32{
				legacyItemOwner: ownerGUID, legacyContainerNumSlots: 16,
			}),
			encode: EncodeContainerCreate, movementBits: [3]byte{0, 0, 0},
			movementBytes: 7, valuesBytes: 1 + 12 + 263 + 76, ownerFlags: 0x03,
		},
		{
			name:       "game object",
			guid:       0xf110000001000045,
			legacyType: 5,
			modernType: 8,
			fields: with(map[int]uint32{
				legacyGameObjectDisplayID:    100,
				legacyGameObjectDynamic:      0xffff0009,
				legacyGameObjectRotation + 3: math.Float32bits(1),
				legacyGameObjectBytes1:       5 | 3<<8 | 7<<16 | 255<<24,
			}),
			encode: EncodeGameObjectCreate, movementBits: [3]byte{0x04, 0x20, 0},
			movementBytes: 31, valuesBytes: 1 + 12 + 75,
		},
		{
			name:       "dynamic object",
			guid:       0xf100000001000046,
			legacyType: 6,
			modernType: 9,
			fields: with(map[int]uint32{
				legacyDynamicObjectCaster:  ownerGUID,
				legacyDynamicObjectSpellID: 1234,
				legacyDynamicObjectRadius:  math.Float32bits(8),
			}),
			encode: EncodeDynamicObjectCreate, movementBits: [3]byte{0x04, 0, 0},
			movementBytes: 23, valuesBytes: 1 + 12 + 22,
		},
		{
			name:       "corpse",
			guid:       0xf101000001000047,
			legacyType: 7,
			modernType: 10,
			fields: with(map[int]uint32{
				legacyCorpseOwner: ownerGUID, legacyCorpseDisplayID: 49,
			}),
			encode: EncodeCorpseCreate, movementBits: [3]byte{0x04, 0, 0},
			movementBytes: 23, valuesBytes: 1 + 12 + 108,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update := LegacyObjectUpdate{
				Type: LegacyUpdateCreateObject2, GUID: test.guid, ObjectType: test.legacyType,
				Movement: &LegacyMovement{UpdateFlags: legacyUpdateStationary, X: 1, Y: 2, Z: 3, Orientation: 0.5},
				Values:   LegacyUpdateValuesBlock{Fields: test.fields},
			}
			body, err := test.encode(update, ActivePlayerCreateOptions{MapID: 571, OwnerGUID: ownerGUID})
			if err != nil {
				t.Fatal(err)
			}
			object := body[11:]
			_, _, guidBytes, err := readPackedGUID128(object[1:])
			if err != nil {
				t.Fatal(err)
			}
			position := 1 + guidBytes
			if object[position] != test.modernType {
				t.Fatalf("modern type = %d, want %d", object[position], test.modernType)
			}
			position++
			if got := [3]byte(object[position : position+3]); got != test.movementBits {
				t.Fatalf("create movement bits = % x, want % x", got, test.movementBits)
			}
			position += test.movementBytes
			valuesLength := int(binary.LittleEndian.Uint32(object[position:]))
			position += 4
			if valuesLength != test.valuesBytes || valuesLength != len(object)-position {
				t.Fatalf("values bytes = %d, want %d (remaining %d)", valuesLength, test.valuesBytes, len(object)-position)
			}
			if object[position] != test.ownerFlags {
				t.Fatalf("visibility flags = 0x%02x, want 0x%02x", object[position], test.ownerFlags)
			}
			if test.legacyType == 5 {
				if got := binary.LittleEndian.Uint32(object[position+5:]); got != 0xffff0024 {
					t.Fatalf("modern GameObject dynamic flags = 0x%08x, want 0xffff0024", got)
				}
				if got := binary.LittleEndian.Uint32(object[position+25:]); got != modernGameObjectStateAnimID {
					t.Fatalf("modern GameObject StateAnimID = %d, want %d", got, modernGameObjectStateAnimID)
				}
				if got := object[position+71]; got != 0xff {
					t.Fatalf("modern GameObject PercentHealth = %d, want 255", got)
				}
			}
		})
	}
}

func TestDynamicObjectCreateGroundVisual(t *testing.T) {
	for _, tc := range []struct{ spell, visual uint32 }{
		{43265, 322437}, {49936, 344081}, {49937, 344082}, {49938, 343668},
		{0xffffffff, 0}, // Unknown spells retain the safe fallback.
	} {
		t.Run(fmt.Sprint(tc.spell), func(t *testing.T) {
			body, err := EncodeDynamicObjectCreate(LegacyObjectUpdate{
				Type: LegacyUpdateCreateObject2, GUID: 0xf100000001000046, ObjectType: 6,
				Movement: &LegacyMovement{UpdateFlags: legacyUpdateStationary, X: 1, Y: 2, Z: 3},
				Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{
					legacyDynamicObjectCaster: 0x42, legacyDynamicObjectSpellID: tc.spell,
					legacyDynamicObjectRadius: math.Float32bits(10), legacyDynamicObjectCastTime: 123456,
				}},
			}, ActivePlayerCreateOptions{MapID: 571})
			if err != nil {
				t.Fatal(err)
			}
			object := body[11:]
			_, _, guidBytes, err := readPackedGUID128(object[1:])
			if err != nil {
				t.Fatal(err)
			}
			position := 1 + guidBytes + 1 + 23
			values := object[position+4:]
			if int(binary.LittleEndian.Uint32(object[position:])) != len(values) {
				t.Fatal("incorrect values length")
			}
			dynamic := values[13:] // Visibility byte followed by ObjectData.
			_, _, casterBytes, err := readPackedGUID128(dynamic)
			if err != nil {
				t.Fatal(err)
			}
			fields := dynamic[casterBytes+1:]
			want := []uint32{tc.visual, tc.spell, math.Float32bits(10), 123456}
			if len(fields) != 4*len(want) {
				t.Fatalf("dynamic fields length = %d", len(fields))
			}
			for i, expected := range want {
				if got := binary.LittleEndian.Uint32(fields[i*4:]); got != expected {
					t.Fatalf("dynamic field %d = %d, want %d", i, got, expected)
				}
			}
		})
	}
}

func TestNonOwnerItemOmitsOwnerOnlyFields(t *testing.T) {
	values := legacyActivePlayerValues{fields: map[int]uint32{legacyItemOwner: 0x42}}
	if got := len(values.appendItem(nil, 571, true)); got != 263 {
		t.Fatalf("owner Item descriptor bytes = %d, want 263", got)
	}
	if got := len(values.appendItem(nil, 571, false)); got != 212 {
		t.Fatalf("non-owner Item descriptor bytes = %d, want 212", got)
	}
}

func TestGameObjectDynamicFlagsSupplyOrdinaryObjectProgress(t *testing.T) {
	if got := modernGameObjectDynamicFlags(0, false); got != 0xffff0000 {
		t.Fatalf("ordinary GameObject dynamic flags = %#x, want completed progress", got)
	}
	if got := modernGameObjectDynamicFlags(0, true); got != 0x40 {
		t.Fatalf("transport dynamic flags = %#x, want active transport bit", got)
	}
}

func TestTransportDefaultsMatchProtocol80(t *testing.T) {
	values := legacyActivePlayerValues{fields: map[int]uint32{
		legacyObjectEntry:            20808,
		legacyGameObjectFlags:        0x28,
		legacyGameObjectLevel:        0,
		legacyGameObjectBytes1:       0xff000f01,
		legacyGameObjectRotation + 3: math.Float32bits(1),
	}}
	data := values.appendGameObjectCreate(nil, 0, true)
	// GameObjectData starts with six uint32 values and two empty packed GUIDs.
	if got := binary.LittleEndian.Uint32(data[28:]); got != modernTransportGameObjectFlags {
		t.Fatalf("transport GameObject flags = %#x, want %#x", got, modernTransportGameObjectFlags)
	}
	if got := binary.LittleEndian.Uint32(data[52:]); got != 0 {
		t.Fatalf("transport level = %d, want legacy proxy protocol-80 zero value", got)
	}
	if got := transportDynamicFlags(1234, transportPeriod(20808)); got == 0 {
		t.Fatal("transport path progress was not encoded")
	}
}

func TestTransportPeriodsIncludeLoginWMOEntries(t *testing.T) {
	for entry, want := range map[uint32]uint32{
		186238: 302415,
		190549: 566367,
	} {
		if got := transportPeriod(entry); got != want {
			t.Fatalf("transport period for %d = %d, want %d", entry, got, want)
		}
	}
}

func TestTransportParentRotationUsesElevatorPivot(t *testing.T) {
	values := legacyActivePlayerValues{fields: map[int]uint32{legacyObjectEntry: 183177}}
	data := values.appendGameObjectCreate(nil, 0, true)
	// Skip display/visual fields, effects count, and the two packed GUIDs.
	const rotationOffset = 32
	for index, want := range []float32{0, 0, -0.69465846, 0.7193397} {
		got := math.Float32frombits(binary.LittleEndian.Uint32(data[rotationOffset+index*4:]))
		if math.Abs(float64(got-want)) > 1e-6 {
			t.Fatalf("elevator parent rotation[%d] = %f, want %f", index, got, want)
		}
	}
}

func TestGameObjectParentRotationDoesNotReuseLegacyLocalRotation(t *testing.T) {
	values := legacyActivePlayerValues{fields: map[int]uint32{
		legacyGameObjectRotation:     math.Float32bits(0.25),
		legacyGameObjectRotation + 1: math.Float32bits(0.5),
		legacyGameObjectRotation + 2: math.Float32bits(0.75),
		legacyGameObjectRotation + 3: math.Float32bits(0.125),
	}}
	data := values.appendGameObjectCreate(nil, 0, false)
	const rotationOffset = 32
	for index, want := range []float32{0, 0, 0, 1} {
		got := math.Float32frombits(binary.LittleEndian.Uint32(data[rotationOffset+index*4:]))
		if got != want {
			t.Fatalf("ParentRotation[%d] = %f, want %f", index, got, want)
		}
	}
}

func TestArthasPlatformParentRotationMatchesCapture(t *testing.T) {
	// Reference capture update-000116-49871.bin object 202161.
	move := &LegacyMovement{
		X: math.Float32frombits(0x43fbcf5c), Y: math.Float32frombits(0xc504ca8f),
		Z: math.Float32frombits(0x445126d9), Orientation: math.Float32frombits(0x40490fd0),
		PackedRotation: 0x1000,
	}
	update := LegacyObjectUpdate{
		Type: LegacyUpdateCreateObject2, GUID: 0xf1100315b1000035, ObjectType: 5,
		Movement: move,
		Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{
			legacyObjectEntry:            goArthasPlatform,
			legacyObjectScale:            math.Float32bits(1),
			legacyGameObjectDisplayID:    9276,
			legacyGameObjectFlags:        0x20,
			legacyGameObjectFaction:      1375,
			legacyGameObjectBytes1:       0xff002101,
			legacyGameObjectRotation:     5535469,
			legacyGameObjectRotation + 1: 0,
			legacyGameObjectRotation + 2: 0,
			legacyGameObjectRotation + 3: 0,
		}},
	}
	body, err := EncodeGameObjectCreate(update, ActivePlayerCreateOptions{MapID: 631})
	if err != nil {
		t.Fatal(err)
	}
	got := gameObjectCreateRotation(t, body)
	want := [4]uint32{arthasPlatformParentRotationX, 0, 0, math.Float32bits(1)}
	if got != want {
		t.Fatalf("Arthas platform parent rotation = %v, want legacy proxy %v", got, want)
	}
}

func TestArthasPrecipiceParentRotationStaysIdentity(t *testing.T) {
	values := legacyActivePlayerValues{fields: map[int]uint32{
		legacyObjectEntry:      202078,
		legacyGameObjectBytes1: 0xff002101,
	}}
	data := values.appendGameObjectCreate(nil, 0, false)
	const rotationOffset = 32
	for index, want := range []float32{0, 0, 0, 1} {
		got := math.Float32frombits(binary.LittleEndian.Uint32(data[rotationOffset+index*4:]))
		if got != want {
			t.Fatalf("precipice ParentRotation[%d] = %f, want identity %f", index, got, want)
		}
	}
}

func TestArthasPlatformQueryFillsData18(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, goArthasPlatform)
	legacy = binary.LittleEndian.AppendUint32(legacy, 33)
	legacy = binary.LittleEndian.AppendUint32(legacy, 9276)
	for range 4 {
		legacy = append(legacy, 0)
	}
	legacy = append(legacy, 0, 0, 0)
	for range 24 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(1))
	for range 6 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	body, err := TranslateGameObjectQueryResponse(legacy, GUID128{})
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: body}
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.guid128(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.bit(); err != nil {
		t.Fatal(err)
	}
	r.align()
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	for range 7 {
		if _, err := r.cstring(); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 35; i++ {
		v, err := r.u32()
		if err != nil {
			t.Fatal(err)
		}
		if i == 18 && v != arthasPlatformParentRotationX {
			t.Fatalf("query data[18]=%d, want %d", v, arthasPlatformParentRotationX)
		}
		if i != 18 && v != 0 {
			t.Fatalf("query data[%d]=%d, want 0", i, v)
		}
	}
}

func TestGameObjectPercentHealthCopiesLegacyAnimationProgress(t *testing.T) {
	values := legacyActivePlayerValues{fields: map[int]uint32{
		legacyGameObjectBytes1: 0x64000b01,
	}}
	data := values.appendGameObjectCreate(nil, 0, true)
	if got := data[58]; got != 100 {
		t.Fatalf("modern PercentHealth = %d, want legacy animation progress 100", got)
	}
}

func TestEncodeGameObjectCreateSupportsTransports(t *testing.T) {
	tests := []struct {
		name string
		guid uint64
		move LegacyMovement
		bits [3]byte
	}{
		{name: "map transport", guid: 0xf120000001000001, move: LegacyMovement{TransportPathTime: 1234}, bits: [3]byte{0x05, 0x20, 0}},
		{name: "moving transport", guid: 0x1fc0000000000001, move: LegacyMovement{TransportPathTime: 5678}, bits: [3]byte{0x05, 0x20, 0}},
		{name: "passenger platform", guid: 0xf110000001000001, move: LegacyMovement{TransportGUID: 0x1fc0000000000002, TransportSeat: -1}, bits: [3]byte{0x0c, 0x20, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := EncodeGameObjectCreate(LegacyObjectUpdate{
				Type: LegacyUpdateCreateObject2, GUID: test.guid, ObjectType: 5, Movement: &test.move,
				Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{legacyObjectEntry: 1}},
			}, ActivePlayerCreateOptions{MapID: 0})
			if err != nil {
				t.Fatal(err)
			}
			object := body[11:]
			_, _, guidBytes, err := readPackedGUID128(object[1:])
			if err != nil {
				t.Fatal(err)
			}
			position := 1 + guidBytes + 1
			if got := [3]byte(object[position : position+3]); got != test.bits {
				t.Fatalf("movement bits=% x, want % x", got, test.bits)
			}
		})
	}
}

func TestPurplePrincessCreateMatchesCapturedBytes(t *testing.T) {
	update := LegacyObjectUpdate{
		Type: LegacyUpdateCreateObject1, GUID: 0x1fc0000000000001, ObjectType: 5,
		Movement: &LegacyMovement{
			X: math.Float32frombits(0xc641db90), Y: math.Float32frombits(0x4353d722),
			Z: math.Float32frombits(0x4246cb4b), Orientation: math.Float32frombits(0x3fcde875),
			TransportPathTime: 383255,
		},
		Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{
			legacyObjectEntry:         176495,
			legacyObjectScale:         math.Float32bits(1),
			legacyGameObjectDisplayID: 3031,
			legacyGameObjectFlags:     0x28,
			legacyGameObjectDynamic:   0x37890010,
			legacyGameObjectLevel:     314929,
			legacyGameObjectBytes1:    0xff000f01,
		}},
	}
	body, err := EncodeGameObjectCreate(update, ActivePlayerCreateOptions{MapID: 0})
	if err != nil {
		t.Fatal(err)
	}
	want, err := hex.DecodeString("0100904018080520000000000090db41c622d753434bcb464275e8cd3f17d90500000000000000000058000000006fb10200400089370000803fd70b00000000000000000000ec060000000000000000000000000000280010000000000000000000000000000000803f0000000031ce0400010fff00000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if got := body[11:]; !bytes.Equal(got, want) {
		t.Fatalf("Purple Princess create differs from legacy proxy\n got: %x\nwant: %x", got, want)
	}
}

func TestAirshipPlatformCreateMatchesCapturedBytes(t *testing.T) {
	// Objects [1] and [3] from capture update-000101:
	// the 0xf120 airship platform components.  Legacy realm forwards
	// GAMEOBJECT_DYNAMIC=0xffff0000 (completed-path sentinel) and GAMEOBJECT_FLAGS
	// 0x828/0x28; legacy proxy writes the low-word 0x40 transport-active bit on top.
	//
	// ParentRotation is excluded from the byte comparison: legacy proxy unpacks the
	// movement rotation (e.g. (0,0,-0.00436,0.99999) for 20656) into the modern
	// ParentRotation quaternion, while this proxy still writes identity there.
	// That rotation path is a separate known discrepancy; every other field is
	// asserted byte-for-byte against the capture.
	tests := []struct {
		name  string
		guid  uint64
		entry uint32
		flags uint32
		rot   uint64
		posX  uint32
		posY  uint32
		posZ  uint32
		ori   uint32
		want  string
	}{
		{
			name: "platform 20656", guid: 0xf120000000000001, entry: 20656, flags: 0x828,
			rot: 0x1fee20, posX: 0x44c2299a, posY: 0x4370a7f0, posZ: 0x425d94af, ori: 0x40c8c85d,
			want: "01009fffffffff401808052000000000009a29c244f0a77043af945d425dc8c8405931000020ee1f00000000005800000000b05000004000ffff0000803fce0100000000000000000000ec0600000000000000000000000000002808000000000000000000009bfb8ebb58ff7f3f0000000000000000010b6400000000000000000000000000000000",
		},
		{
			name: "platform 20654", guid: 0xf120000000000007, entry: 20654, flags: 0x28,
			rot: 0x14a316, posX: 0x44c766b8, posY: 0x4332b168, posZ: 0xc22216d6, ori: 0x4096846e,
			want: "0100bfffffffffc001180805200000000000b866c74468b13243d61622c26e8496405931000016a31400000000005800000000ae5000004000ffff0000803fce0100000000000000000000ec0600000000000000000000000000002800000000000000000000009ece35bf643a343f0000000000000000010b6400000000000000000000000000000000",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update := LegacyObjectUpdate{
				Type: LegacyUpdateCreateObject1, GUID: test.guid, ObjectType: 5,
				Movement: &LegacyMovement{
					X: math.Float32frombits(test.posX), Y: math.Float32frombits(test.posY),
					Z: math.Float32frombits(test.posZ), Orientation: math.Float32frombits(test.ori),
					TransportPathTime: 12633, PackedRotation: test.rot,
				},
				Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{
					legacyObjectEntry:         test.entry,
					legacyObjectScale:         math.Float32bits(1),
					legacyGameObjectDisplayID: 462,
					legacyGameObjectFlags:     test.flags,
					legacyGameObjectDynamic:   0xffff0000,
					legacyGameObjectLevel:     0,
					legacyGameObjectBytes1:    0x64000b01,
				}},
			}
			body, err := EncodeGameObjectCreate(update, ActivePlayerCreateOptions{MapID: 0})
			if err != nil {
				t.Fatal(err)
			}
			want, err := hex.DecodeString(test.want)
			if err != nil {
				t.Fatal(err)
			}
			got := body[11:]
			// Normalize the ParentRotation quaternion (rot0-rot3) in both sides.
			const valuesLen = 88
			for i := len(got) - valuesLen + 45; i < len(got)-valuesLen+61; i++ {
				got[i], want[i] = 0, 0
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("airship platform create differs from legacy proxy\n got: %x\nwant: %x", got, want)
			}
		})
	}
}

func TestMOTransportShipFlagsMatchWhenPeriodUnknown(t *testing.T) {
	// legacy proxy stamps 0x100028 on every MO_TRANSPORT ship root even when the entry
	// has no DB2 path period (181689 in the capture, plus 181688/190536 in the
	// live proxy log).  Gating the flag on the period table left these ships
	// with 0x28 and made the 3.4.3 client render them as plain objects.
	tests := []struct {
		entry  uint32
		packed uint32 // GAMEOBJECT_BYTES_1 with TypeID 15 (MO_TRANSPORT)
	}{
		{entry: 181689, packed: 0xff000f01},
		{entry: 190536, packed: 0xff000f01},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("entry-%d", test.entry), func(t *testing.T) {
			values := legacyActivePlayerValues{fields: map[int]uint32{
				legacyObjectEntry:      test.entry,
				legacyGameObjectBytes1: test.packed,
				legacyGameObjectFlags:  0x28,
			}}
			data := values.appendGameObjectCreate(nil, 0, true)
			// displayID + 4 visual fields + worldFxCount (24 bytes) + two packed GUIDs (4).
			if got := binary.LittleEndian.Uint32(data[28:]); got != modernTransportGameObjectFlags {
				t.Fatalf("MO_TRANSPORT ship flags = %#x, want %#x", got, modernTransportGameObjectFlags)
			}
		})
	}
}

func TestIsLegacyTransportGameObject(t *testing.T) {
	tests := []struct {
		name   string
		update LegacyObjectUpdate
		want   bool
	}{
		{name: "moving transport root", update: LegacyObjectUpdate{ObjectType: 5, GUID: 0x1fc0000000000001, Movement: &LegacyMovement{}}, want: true},
		{name: "map transport root", update: LegacyObjectUpdate{ObjectType: 5, GUID: 0xf120000001000001, Movement: &LegacyMovement{}}, want: true},
		{name: "transport passenger", update: LegacyObjectUpdate{ObjectType: 5, GUID: 0xf110000001000001, Movement: &LegacyMovement{TransportGUID: 0x1fc0000000000001}}, want: true},
		{name: "ordinary GameObject", update: LegacyObjectUpdate{ObjectType: 5, GUID: 0xf110000001000001, Movement: &LegacyMovement{}}, want: false},
		{name: "unit on transport", update: LegacyObjectUpdate{ObjectType: 3, GUID: 0xf130000001000001, Movement: &LegacyMovement{TransportGUID: 0x1fc0000000000001}}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsLegacyTransportGameObject(test.update); got != test.want {
				t.Fatalf("IsLegacyTransportGameObject() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestGameObjectTransportInfoPresenceBitsMatchModernOrder(t *testing.T) {
	move := LegacyMovement{
		TransportGUID: 0x1fc0000000000002, TransportSeat: -1,
		TransportTime: 66, TransportTime2: 44,
	}
	withLegacyPrevTime := appendNonUnitCreateMovement(nil, LegacyObjectUpdate{
		GUID: 0xf110000001000001, Movement: &move,
	}, true, ActivePlayerCreateOptions{})
	move.TransportTime2 = 0
	withoutLegacyPrevTime := appendNonUnitCreateMovement(nil, LegacyObjectUpdate{
		GUID: 0xf110000001000001, Movement: &move,
	}, true, ActivePlayerCreateOptions{})
	if len(withLegacyPrevTime) != len(withoutLegacyPrevTime) {
		t.Fatalf("legacy TransportTime2 changed encoded length: with=%d without=%d", len(withLegacyPrevTime), len(withoutLegacyPrevTime))
	}
	if got := withLegacyPrevTime[len(withLegacyPrevTime)-1]; got != 0 {
		t.Fatalf("transport presence bits = 0x%02x, want HasPrevTime/HasVehicleID clear", got)
	}
	if string(withLegacyPrevTime) != string(withoutLegacyPrevTime) {
		t.Fatalf("legacy TransportTime2 altered modern transport encoding")
	}
}

func TestGameObjectCreateKeepsIdentityPackedRotation(t *testing.T) {
	move := LegacyMovement{Orientation: math.Pi, PackedRotation: 0}
	data := appendNonUnitCreateMovement(nil, LegacyObjectUpdate{
		GUID: 0x1fc0000000000001, Movement: &move,
	}, true, ActivePlayerCreateOptions{})
	// 3 movement-bit bytes, pause count, stationary position, and server time.
	const rotationOffset = 3 + 4 + 16 + 4
	if got := binary.LittleEndian.Uint64(data[rotationOffset:]); got != 0 {
		t.Fatalf("identity local rotation = 0x%x, want zero", got)
	}
}

func TestTransportCreateFallsBackToUnixWhenPathTimerIsZero(t *testing.T) {
	// legacy proxy writes Unix seconds whenever path_time is 0, including ICC
	// gunships that carry UPDATEFLAG_TRANSPORT with an explicit zero.
	tests := []LegacyMovement{
		{UpdateFlags: legacyUpdateTransport, TransportPathTime: 0},
		{TransportPathTime: 0},
	}
	const pathTimeOffset = 3 + 4 + 16
	for _, move := range tests {
		move := move
		data := appendNonUnitCreateMovement(nil, LegacyObjectUpdate{
			GUID: 0x1fc0000000000015, Movement: &move,
		}, true, ActivePlayerCreateOptions{MapID: 631})
		if got := binary.LittleEndian.Uint32(data[pathTimeOffset:]); got == 0 {
			t.Fatalf("update_flags=0x%x encoded path_time=0, want Unix fallback", move.UpdateFlags)
		}
	}
}

func TestTransportPathTimeMatchesClockStamp(t *testing.T) {
	// The ICC gunship hulls arrive with no realm timer. legacy proxy stamps the local
	// clock there, and a zero stamp instead ran both hulls to the end of their
	// paths on the live realm, so the fallback has to stay the wall clock.
	move := LegacyMovement{UpdateFlags: legacyUpdateTransport}
	if got := transportPathTime(&move, time.Unix(1789719376, 0)); got != 1789719376 {
		t.Fatalf("hull path time=%d, want the clock stamp 1789719376", got)
	}
	// The static ICC lifts do carry one, and it must survive untouched.
	move.TransportPathTime = 25426968
	if got := transportPathTime(&move, time.Unix(1789719376, 0)); got != 25426968 {
		t.Fatalf("realm path time overridden: %d", got)
	}
}

func TestGameObjectStationaryOrientationMatchesNormalizedAngle(t *testing.T) {
	move := LegacyMovement{Orientation: -1.466080}
	data := appendNonUnitCreateMovement(nil, LegacyObjectUpdate{
		GUID: 0xf110000001000001, Movement: &move,
	}, true, ActivePlayerCreateOptions{})
	const orientationOffset = 3 + 4 + 3*4
	got := math.Float32frombits(binary.LittleEndian.Uint32(data[orientationOffset:]))
	want := clampOrientation(move.Orientation)
	if got != want || got < 0 {
		t.Fatalf("stationary orientation = %f, want normalized %f", got, want)
	}
}

func TestGameObjectCreateRotationValuesOverrideMovementQuaternion(t *testing.T) {
	move := LegacyMovement{PackedRotation: packGameObjectRotation([4]float64{0, 0, math.Sin(math.Pi / 4), math.Cos(math.Pi / 4)})}
	update := LegacyObjectUpdate{
		GUID: 0x1fc0000000000001, Movement: &move,
		Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{
			legacyGameObjectRotation:     math.Float32bits(0),
			legacyGameObjectRotation + 1: math.Float32bits(0),
			legacyGameObjectRotation + 2: math.Float32bits(0),
			legacyGameObjectRotation + 3: math.Float32bits(1),
		}},
	}
	data := appendNonUnitCreateMovement(nil, update, true, ActivePlayerCreateOptions{})
	const rotationOffset = 3 + 4 + 16 + 4
	if got := binary.LittleEndian.Uint64(data[rotationOffset:]); got != 0 {
		t.Fatalf("values identity rotation = 0x%x, want zero", got)
	}
}

func TestDeeprunTramLocalRotationInvertsYaw(t *testing.T) {
	move := LegacyMovement{PackedRotation: packGameObjectRotation([4]float64{
		0, 0, math.Sin(math.Pi / 8), math.Cos(math.Pi / 8),
	})}
	update := LegacyObjectUpdate{
		GUID: 0x1fc0000000000001, Movement: &move,
		Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{
			legacyObjectEntry: 176080,
		}},
	}
	got := gameObjectPackedRotation(update)
	original, ok := unpackGameObjectRotation(move.PackedRotation)
	if !ok {
		t.Fatal("test rotation did not unpack")
	}
	want := packGameObjectRotation(invertGameObjectYaw(original))
	if got != want {
		t.Fatalf("tram local rotation = %#x, want yaw-inverted %#x", got, want)
	}
}

func TestPackedGameObjectRotationRoundTrip(t *testing.T) {
	want := [4]float64{0.25, -0.5, 0.125, math.Sqrt(1 - 0.25*0.25 - 0.5*0.5 - 0.125*0.125)}
	packed := packGameObjectRotation(want)
	got, ok := unpackGameObjectRotation(packed)
	if !ok {
		t.Fatal("valid packed quaternion did not unpack")
	}
	for index := range want {
		if math.Abs(got[index]-want[index]) > 1e-5 {
			t.Fatalf("quaternion[%d] = %f, want %f", index, got[index], want[index])
		}
	}
}

func TestTransportCreateDynamicFlagsMatchPresenceSemantics(t *testing.T) {
	const pathTime = 1234
	period := transportPeriod(20808)
	if got := modernGameObjectCreateDynamicFlags(0x37890001, true, true, pathTime, period); got != 0x37890044 {
		t.Fatalf("explicit transport dynamic flags = %#x, want preserved progress and active transport bit", got)
	}
	if got := modernGameObjectCreateDynamicFlags(0, false, true, pathTime, period); got != transportDynamicFlags(pathTime, period)|0x40 {
		t.Fatalf("implicit transport progress = %#x, want %#x", got, transportDynamicFlags(pathTime, period)|0x40)
	}
	if got := modernGameObjectCreateDynamicFlags(0x12340001, true, false, pathTime, period); got != 0xffff0004 {
		t.Fatalf("ordinary create dynamic flags = %#x, want completed progress", got)
	}
}

func TestLegacyTransportGUIDUsesMissingEntrySentinel(t *testing.T) {
	guid := ModernGUIDForLegacy(0xf120000000001e03, 0)
	wantHigh := uint64(6)<<58 | uint64(0x1e03)<<38 | math.MaxUint32
	if guid.Low != 0 || guid.High != wantHigh {
		t.Fatalf("modern transport GUID = %016x:%016x, want %016x:%016x", guid.High, guid.Low, wantHigh, uint64(0))
	}
}

func TestICCGunshipPreservesServerPosition(t *testing.T) {
	for _, entry := range []uint32{201580, 201581, 201811, 201812} {
		move := LegacyMovement{X: -69.16, Y: 1992.55, Z: 584.56, Orientation: 1.25}
		update := LegacyObjectUpdate{Type: LegacyUpdateCreateObject2, GUID: 0x1fc0000000000016, ObjectType: 5, Movement: &move,
			Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{legacyObjectEntry: entry, legacyGameObjectBytes1: 0xff000f00, legacyGameObjectDynamic: 0}}}
		body, err := EncodeGameObjectCreate(update, ActivePlayerCreateOptions{MapID: 631})
		if err != nil {
			t.Fatal(err)
		}
		x, y, z, o := gameObjectCreateStationary(t, body)
		if x != move.X || y != move.Y || z != move.Z || o != move.Orientation {
			t.Fatalf("entry %d: server pose overwritten", entry)
		}
		object := body[11:]
		_, _, n, err := readPackedGUID128(object[1:])
		if err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint32(object[1+n+1+3+4+16:]) == 0 {
			t.Fatal("missing legacy proxy Unix timer fallback")
		}
	}
}

func gameObjectCreateStationary(t *testing.T, body []byte) (x, y, z, o float32) {
	t.Helper()
	object := body[11:]
	_, _, guidBytes, err := readPackedGUID128(object[1:])
	if err != nil {
		t.Fatal(err)
	}
	const pauseBytes = 4
	offset := 1 + guidBytes + 1 + 3 + pauseBytes
	if len(object) < offset+16 {
		t.Fatalf("create body too short for stationary position: %d", len(object))
	}
	x = math.Float32frombits(binary.LittleEndian.Uint32(object[offset:]))
	y = math.Float32frombits(binary.LittleEndian.Uint32(object[offset+4:]))
	z = math.Float32frombits(binary.LittleEndian.Uint32(object[offset+8:]))
	o = math.Float32frombits(binary.LittleEndian.Uint32(object[offset+12:]))
	return
}

func TestTransportParentRotationUsesLegacyRotation(t *testing.T) {
	// Capture 202220/202234/196840 are the 0xf120 map platforms; legacy proxy gives
	// them ParentRotation (0, 0, 1, ~0) while the legacy realm's local packed
	// rotation is not identity either. The create must forward the legacy
	// GAMEOBJECT_ROTATION words rather than defaulting to the identity.
	move := &LegacyMovement{PackedRotation: 0x100000, X: 1, Y: 2, Z: 3}
	legacy := [4]uint32{0, 0, math.Float32bits(1), math.Float32bits(1.2706e-06)}
	update := LegacyObjectUpdate{Type: LegacyUpdateCreateObject1, GUID: 0xf120000000000001, ObjectType: 5,
		Movement: move, Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{
			legacyObjectEntry: 202220, legacyGameObjectBytes1: 0x64000b00,
			legacyGameObjectRotation:     legacy[0],
			legacyGameObjectRotation + 1: legacy[1],
			legacyGameObjectRotation + 2: legacy[2],
			legacyGameObjectRotation + 3: legacy[3],
		}}}
	body, err := EncodeGameObjectCreate(update, ActivePlayerCreateOptions{MapID: 631})
	if err != nil {
		t.Fatal(err)
	}
	got := gameObjectCreateRotation(t, body)
	if got != legacy {
		t.Fatalf("platform parent rotation = %v, want %v", got, legacy)
	}
}

func TestTransportParentRotationFallsBackToIdentity(t *testing.T) {
	move := &LegacyMovement{PackedRotation: 0, X: 1, Y: 2, Z: 3}
	update := LegacyObjectUpdate{Type: LegacyUpdateCreateObject1, GUID: 0x1fc0000000000015, ObjectType: 5,
		Movement: move, Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{
			legacyObjectEntry: 201580, legacyGameObjectBytes1: 0xff000f00,
		}}}
	body, err := EncodeGameObjectCreate(update, ActivePlayerCreateOptions{MapID: 631})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := gameObjectCreateRotation(t, body), [4]uint32{0, 0, 0, math.Float32bits(1)}; got != want {
		t.Fatalf("hull parent rotation = %v, want identity %v", got, want)
	}
}

// gameObjectCreateRotation reads the four ParentRotation words that follow the
// create's stationary position, server time, and move rotation.
func gameObjectCreateRotation(t *testing.T, body []byte) [4]uint32 {
	t.Helper()
	object := body[11:]
	// GameObjectData.ParentRotation is the four UInt32 that follow the create
	// flags. The words behind it are fixed - faction, level, state, type,
	// percent health, art kit and three trailing UInt32 - so anchor at the end
	// rather than re-walking the variable-length GUIDs ahead of it.
	const tailAfterRotation = 4 + 4 + 1 + 1 + 1 + 4 + 12
	offset := len(object) - tailAfterRotation - 16
	if offset < 0 {
		t.Fatalf("create body too short for a parent rotation: %d", len(object))
	}
	var rotation [4]uint32
	for index := range rotation {
		rotation[index] = binary.LittleEndian.Uint32(object[offset+index*4:])
	}
	return rotation
}

func TestPetCreateGUIDKeepsPetNumberEntry(t *testing.T) {
	update := LegacyObjectUpdate{
		Type: LegacyUpdateCreateObject2, GUID: 0xf14000004d000042, ObjectType: 3,
		Movement: &LegacyMovement{UpdateFlags: legacyUpdateLiving},
		Values:   LegacyUpdateValuesBlock{Fields: map[int]uint32{legacyObjectEntry: 12345}},
	}
	low, high := modernLegacyCreateGUID(update, 571)
	if low != 0x42 {
		t.Fatalf("pet counter = 0x%x, want 0x42", low)
	}
	// legacy proxy keeps pet_number (0x4d) in the GUID entry. The creature template
	// remains available through ObjectData.EntryID (legacyObjectEntry). The map
	// field at bits 29-41 stays clear, like the creature and GameObject families.
	wantHigh := uint64(10)<<58 | uint64(1)<<42 | uint64(0x4d)<<6
	if high != wantHigh {
		t.Fatalf("pet high GUID = 0x%x, want 0x%x", high, wantHigh)
	}
	if generic := ModernGUIDForLegacy(update.GUID, 571); generic != (GUID128{Low: low, High: high}) {
		t.Fatalf("pet create GUID %#v differs from summon/pet-packet GUID %#v", GUID128{Low: low, High: high}, generic)
	}
}

func TestICCSkybreakerCreateMatchesCapture(t *testing.T) {
	want, err := os.ReadFile("testdata/icc-skybreaker-20260917.bin")
	if err != nil {
		t.Fatal(err)
	}
	fields := map[int]uint32{
		legacyObjectEntry: 201580, legacyObjectScale: math.Float32bits(1), legacyGameObjectDisplayID: 9150,
		legacyGameObjectBytes1: 0xff000f00, legacyGameObjectDynamic: 0, legacyGameObjectLevel: 0x13169,
	}
	update := LegacyObjectUpdate{Type: LegacyUpdateCreateObject1, GUID: 0x1fc0000000000015, ObjectType: 5,
		Movement: &LegacyMovement{UpdateFlags: 0x252, X: -459.1007080078125, Y: 2466.109375, Z: 169.8642578125, Orientation: 3.0971834659576416},
		Values:   LegacyUpdateValuesBlock{Fields: fields}}
	got, err := EncodeGameObjectCreate(update, ActivePlayerCreateOptions{MapID: 631})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("length %d want %d", len(got), len(want))
	}
	if byte(fields[legacyGameObjectBytes1]) != 1 {
		t.Fatalf("dock create state = %d, want parked so 3.4.3 does not start the Saurfang path", byte(fields[legacyGameObjectBytes1]))
	}
	// Unix timestamp differs per session. legacy proxy captured the realm's moving
	// create; the dock spawn is rewritten to parked (dynamic 0x40, GO state 1)
	// so a later park-at-progress-0 Values update cannot be read as path-complete.
	copy(got[41:45], want[41:45])
	want[0x3e] = 0x40
	want[0x3f] = 0
	want[0x40] = 0
	want[0x41] = 0
	want[0x7e] = 1
	if !bytes.Equal(got, want) {
		t.Fatalf("create differs from parked legacy proxy:\ngot  %x\nwant %x", got, want)
	}
}

func TestICCGunshipDockCreateParksMovingSpawn(t *testing.T) {
	fieldsFor := func() map[int]uint32 {
		return map[int]uint32{
			legacyObjectEntry: 201580, legacyGameObjectBytes1: 0xff000f00,
			legacyGameObjectDynamic: 0, legacyGameObjectLevel: 0x13169,
		}
	}
	dockFields := fieldsFor()
	dock := LegacyObjectUpdate{Type: LegacyUpdateCreateObject1, GUID: 0x1fc0000000000015, ObjectType: 5,
		Movement: &LegacyMovement{X: -459.1, Y: 2466.1, Z: 169.86, Orientation: 3.097},
		Values:   LegacyUpdateValuesBlock{Fields: dockFields}}
	if _, err := EncodeGameObjectCreate(dock, ActivePlayerCreateOptions{MapID: 631}); err != nil {
		t.Fatal(err)
	}
	if byte(dockFields[legacyGameObjectBytes1]) != 1 {
		t.Fatalf("dock moving spawn stayed state %d", byte(dockFields[legacyGameObjectBytes1]))
	}

	airFields := fieldsFor()
	airFields[legacyObjectEntry] = 201581
	air := LegacyObjectUpdate{Type: LegacyUpdateCreateObject1, GUID: 0x1fc0000000000016, ObjectType: 5,
		Movement: &LegacyMovement{X: -69.16, Y: 1992.55, Z: 584.56, Orientation: 0},
		Values:   LegacyUpdateValuesBlock{Fields: airFields}}
	if _, err := EncodeGameObjectCreate(air, ActivePlayerCreateOptions{MapID: 631}); err != nil {
		t.Fatal(err)
	}
	if byte(airFields[legacyGameObjectBytes1]) != 0 {
		t.Fatalf("combat-height hull was parked: state %d", byte(airFields[legacyGameObjectBytes1]))
	}

	parkedFields := fieldsFor()
	parkedFields[legacyGameObjectBytes1] = 0xff000f01
	parked := LegacyObjectUpdate{Type: LegacyUpdateCreateObject1, GUID: 0x1fc0000000000015, ObjectType: 5,
		Movement: &LegacyMovement{X: -459.1, Y: 2466.1, Z: 169.86},
		Values:   LegacyUpdateValuesBlock{Fields: parkedFields}}
	StabilizeICCGunshipCreate(&parked)
	if parkedFields[legacyGameObjectBytes1] != 0xff000f01 {
		t.Fatalf("already-parked dock hull rewritten: 0x%x", parkedFields[legacyGameObjectBytes1])
	}
}

func TestICCOrgrimsHammerCreateKeepsPathProgress(t *testing.T) {
	// Orgrim's Hammer becomes visible mid-encounter. legacy proxy capture
	// 20260917-165006 update-001189 shows it stopped at path progress 0x77d6 of
	// its 52492 ms period, and the 3.4.3 client animates the hull from that
	// progress alone: zeroing the high word puts the ship back at its dock and
	// leaves the cannons out of range. The fixture is that create record, the
	// 134 bytes behind the batch header.
	want, err := os.ReadFile("testdata/icc-orgrims-hammer-20260917.bin")
	if err != nil {
		t.Fatal(err)
	}
	update := LegacyObjectUpdate{Type: LegacyUpdateCreateObject1, GUID: 0x1fc0000000000016, ObjectType: 5,
		Movement: &LegacyMovement{UpdateFlags: 0x252,
			X:           math.Float32frombits(0xc3be37c2),
			Y:           math.Float32frombits(0x44f8e50f),
			Z:           math.Float32frombits(0x43d8d39e),
			Orientation: math.Float32frombits(0x39082888)},
		Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{
			legacyObjectEntry: 201581, legacyObjectScale: math.Float32bits(1), legacyGameObjectDisplayID: 9151,
			legacyGameObjectBytes1: 0xff000f01, legacyGameObjectDynamic: 0x77d60000, legacyGameObjectLevel: 0xcd0c,
		}}}
	body, err := EncodeGameObjectCreate(update, ActivePlayerCreateOptions{MapID: 631})
	if err != nil {
		t.Fatal(err)
	}
	// Skip the batch header and patch the per-session Unix server time.
	got := body[11:]
	copy(got[30:34], want[30:34])
	if !bytes.Equal(got, want) {
		t.Fatalf("mid-flight transport create differs from legacy proxy:\ngot  %x\nwant %x", got, want)
	}
}
