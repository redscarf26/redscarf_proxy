package modernworld

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestParseLegacyUpdateObjectValuesAndLists(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 3)
	body = append(body, byte(LegacyUpdateValues))
	body = appendTestPackedGUID(body, 0x0000000000004201)
	body = append(body, 1)
	body = binary.LittleEndian.AppendUint32(body, (1<<1)|(1<<31))
	body = binary.LittleEndian.AppendUint32(body, 0x11111111)
	body = binary.LittleEndian.AppendUint32(body, 0xaaaaaaaa)
	body = append(body, byte(LegacyUpdateFarObjects))
	body = binary.LittleEndian.AppendUint32(body, 2)
	body = appendTestPackedGUID(body, 0x42)
	body = appendTestPackedGUID(body, 0x4000000000000088)
	body = append(body, byte(LegacyUpdateNearObjects))
	body = binary.LittleEndian.AppendUint32(body, 0)

	batch, err := ParseLegacyUpdateObject(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Updates) != 3 {
		t.Fatalf("updates=%d", len(batch.Updates))
	}
	values := batch.Updates[0]
	if values.GUID != 0x4201 || values.Values.Fields[1] != 0x11111111 || values.Values.Fields[31] != 0xaaaaaaaa {
		t.Fatalf("unexpected values update: %#v", values)
	}
	if got := batch.Updates[1].GUIDs; len(got) != 2 || got[0] != 0x42 || got[1] != 0x4000000000000088 {
		t.Fatalf("far GUIDs=%#v", got)
	}
}

func TestParseLegacyPlayerCreateLiving(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = append(body, byte(LegacyUpdateCreateObject2))
	body = appendTestPackedGUID(body, 0x77)
	body = append(body, 4) // ObjectTypeLegacy.Player
	body = binary.LittleEndian.AppendUint16(body, legacyUpdateSelf|legacyUpdateLiving|legacyUpdateAttackingTarget)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint16(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 1234)
	for _, value := range []float32{1, 2, 3, 4} {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(value))
	}
	body = binary.LittleEndian.AppendUint32(body, 55) // fall time
	for _, value := range []float32{2.5, 7, 4.5, 4.72222, 2.5, 7, 4.5, 3.141593, 3.141593} {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(value))
	}
	body = appendTestPackedGUID(body, 0x99)
	body = append(body, 1)
	body = binary.LittleEndian.AppendUint32(body, 1<<4)
	body = binary.LittleEndian.AppendUint32(body, 12345)

	batch, err := ParseLegacyUpdateObject(body)
	if err != nil {
		t.Fatal(err)
	}
	update := batch.Updates[0]
	if update.Type != LegacyUpdateCreateObject2 || update.ObjectType != 4 || update.GUID != 0x77 {
		t.Fatalf("unexpected create header: %#v", update)
	}
	if update.Movement == nil || update.Movement.MoveTime != 1234 || update.Movement.X != 1 || update.Movement.AttackTarget != 0x99 {
		t.Fatalf("unexpected movement: %#v", update.Movement)
	}
	if update.Values.Fields[4] != 12345 {
		t.Fatalf("field 4=%d", update.Values.Fields[4])
	}
}

func TestParseLegacyGameObjectPositionUsesObjectOrientationForTransport(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = append(body, byte(LegacyUpdateCreateObject2))
	body = appendTestPackedGUID(body, 0xf110000001000001)
	body = append(body, 5) // ObjectTypeLegacy.GameObject
	body = binary.LittleEndian.AppendUint16(body, legacyUpdateGOPosition|legacyUpdateTransport|legacyUpdateGORotation)
	body = appendTestPackedGUID(body, 0x1fc0000000000002)
	for _, value := range []float32{1, 2, 3, 4, 5, 6, 7, 8} {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(value))
	}
	body = binary.LittleEndian.AppendUint32(body, 1234)
	body = binary.LittleEndian.AppendUint64(body, 0x1122334455667788)
	body = append(body, 0) // empty values mask

	batch, err := ParseLegacyUpdateObject(body)
	if err != nil {
		t.Fatal(err)
	}
	move := batch.Updates[0].Movement
	if move == nil || move.Orientation != 7 || move.TransportOrientation != 7 || move.CorpseOrientation != 8 {
		t.Fatalf("unexpected GameObject orientations: %#v", move)
	}
	if move.TransportPathTime != 1234 || move.PackedRotation != 0x1122334455667788 {
		t.Fatalf("unexpected GameObject transport data: %#v", move)
	}
}

func TestDecodeLegacyCompressedUpdateObject(t *testing.T) {
	plain := binary.LittleEndian.AppendUint32(nil, 0)
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(plain)))
	body = append(body, compressed.Bytes()...)
	batch, err := DecodeLegacyUpdateObject(LegacySMSGCompressedUpdateObject, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Updates) != 0 {
		t.Fatalf("updates=%d", len(batch.Updates))
	}
}

func TestParseLegacyUpdateObjectRejectsMalformedBodies(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{name: "truncated", body: []byte{1, 0, 0, 0, byte(LegacyUpdateValues)}, want: "unexpected EOF"},
		{name: "unknown type", body: []byte{1, 0, 0, 0, 9}, want: "unknown legacy update type"},
		{name: "trailing", body: []byte{0, 0, 0, 0, 0xff}, want: "trailing bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseLegacyUpdateObject(test.body)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v, want substring %q", err, test.want)
			}
		})
	}
}

func appendTestPackedGUID(dst []byte, guid uint64) []byte {
	maskIndex := len(dst)
	dst = append(dst, 0)
	var mask byte
	for index := uint(0); index < 8; index++ {
		value := byte(guid >> (index * 8))
		if value == 0 {
			continue
		}
		mask |= 1 << index
		dst = append(dst, value)
	}
	dst[maskIndex] = mask
	return dst
}
