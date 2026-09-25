package modernworld

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestAppendCreateSplineDataBuild54261Layout(t *testing.T) {
	spline := &LegacyMovementSpline{
		Flags:        legacySplineFinalTarget | legacySplineCatmullRom | 0x00080000,
		FacingTarget: 0x42,
		Time:         250, FullTime: 1000, ID: 77,
		Points: [][3]float32{{1, 2, 3}, {4, 5, 6}},
		End:    [3]float32{9, 8, 7},
	}
	body, err := appendCreateSplineData(nil, spline, 571)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(body) != 77 {
		t.Fatalf("spline ID=%d", binary.LittleEndian.Uint32(body))
	}
	// Cyclic create splines write a zero destination regardless of legacy End.
	for position := 4; position < 16; position += 4 {
		if binary.LittleEndian.Uint32(body[position:]) != 0 {
			t.Fatalf("cyclic end is not zero: %x", body[4:16])
		}
	}
	if body[16] != 0x80 {
		t.Fatalf("has-spline-move byte=%02x", body[16])
	}
	flags := binary.LittleEndian.Uint32(body[17:])
	if want := uint32(0x00000800 | 0x00001000); flags != want {
		t.Fatalf("modern spline flags=%08x want=%08x", flags, want)
	}
	if binary.LittleEndian.Uint32(body[21:]) != 250 || binary.LittleEndian.Uint32(body[25:]) != 1000 {
		t.Fatalf("spline times malformed: %x", body[21:29])
	}
	// Header through the 24-bit block is 40 bytes; FacingTarget is a packed GUID128.
	low, _, consumed, err := readPackedGUID128(body[40:])
	if err != nil {
		t.Fatal(err)
	}
	if low != 0x42 || consumed == 0 {
		t.Fatalf("facing target low=%x consumed=%d", low, consumed)
	}
	pointOffset := 40 + consumed
	if got := math.Float32frombits(binary.LittleEndian.Uint32(body[pointOffset:])); got != 1 {
		t.Fatalf("first spline point X=%f", got)
	}
}

func TestAppendCreateSplineDataEmptyAndLimit(t *testing.T) {
	body, err := appendCreateSplineData(nil, &LegacyMovementSpline{ID: 5, End: [3]float32{1, 2, 3}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 17 || body[16] != 0 {
		t.Fatalf("empty spline body=%x", body)
	}
	tooMany := &LegacyMovementSpline{Points: make([][3]float32, int(maxMonsterMovePoints)+1)}
	_, err = appendCreateSplineData(nil, tooMany, 0)
	if err == nil || !strings.Contains(err.Error(), "wire limit") {
		t.Fatalf("limit error=%v", err)
	}
}

func TestUnitCreateCarriesCreateTimeSpline(t *testing.T) {
	update := LegacyObjectUpdate{
		Type: LegacyUpdateCreateObject2, GUID: 0xf130000001000043, ObjectType: 3,
		Movement: &LegacyMovement{
			UpdateFlags: legacyUpdateLiving, TransportSeat: -1,
			Spline: &LegacyMovementSpline{ID: 99, FullTime: 500, Points: [][3]float32{{1, 2, 3}}, End: [3]float32{4, 5, 6}},
		},
		Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{legacyObjectEntry: 123}},
	}
	withSpline, err := EncodeUnitCreate(update, ActivePlayerCreateOptions{MapID: 571})
	if err != nil {
		t.Fatal(err)
	}
	update.Movement.Spline = nil
	withoutSpline, err := EncodeUnitCreate(update, ActivePlayerCreateOptions{MapID: 571})
	if err != nil {
		t.Fatal(err)
	}
	if len(withSpline) <= len(withoutSpline) {
		t.Fatalf("create-time spline must be present: with=%d without=%d", len(withSpline), len(withoutSpline))
	}
}

func TestTransportPassengerCreateDropsSpline(t *testing.T) {
	// ICC gunship boarding wave: Kor'kron bolted to Orgrim's Hammer with the
	// jump spline already running. Both live clients dropped with Disconnect 7
	// on this create.
	update := LegacyObjectUpdate{
		Type: LegacyUpdateCreateObject2, GUID: 0xf1300090680002ca, ObjectType: 3,
		Movement: &LegacyMovement{
			UpdateFlags:   legacyUpdateLiving,
			MoveFlags:     legacyMoveSplineEnabled | 0x00000200 | 1,
			TransportGUID: 0x1fc0000000000018, TransportSeat: -1,
			Spline: &LegacyMovementSpline{
				ID: 8962029, Time: 5097, FullTime: 7917,
				Points: [][3]float32{{136.754, -25.265, 44.284}, {-12.093, 27.659, 33.586}},
				End:    [3]float32{-12.093, 27.659, 33.586},
			},
		},
		Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{legacyObjectEntry: 36968}},
	}
	onTransport, err := EncodeUnitCreate(update, ActivePlayerCreateOptions{MapID: 631})
	if err != nil {
		t.Fatal(err)
	}
	update.Movement.Spline = nil
	update.Movement.MoveFlags &^= legacyMoveSplineEnabled
	standing, err := EncodeUnitCreate(update, ActivePlayerCreateOptions{MapID: 631})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(onTransport, standing) {
		t.Fatalf("transport passenger create must match a standing create:\nwith spline: %x\nstanding:    %x", onTransport, standing)
	}
}

func TestSplineFlagWLKCastModernWalkmodeHasNoExtraBits(t *testing.T) {
	if got, want := modernSplineFlags(0x1), modernSplineAnimTierSwim; got != want {
		t.Fatalf("AnimTier Swim: got=%08x want=%08x", got, want)
	}
	if got, want := modernSplineFlags(0x2), modernSplineAnimTierHover; got != want {
		t.Fatalf("AnimTier Hover: got=%08x want=%08x", got, want)
	}
	if got, want := modernSplineFlags(0x3), modernSplineAnimTierFly; got != want {
		t.Fatalf("AnimTier Fly: got=%08x want=%08x", got, want)
	}
	if got, want := modernSplineFlags(0x4), modernSplineAnimTierSubmerged; got != want {
		t.Fatalf("AnimTier Submerged: got=%08x want=%08x", got, want)
	}
	if got := modernSplineFlags(legacySplineCanSwim); got != modernSplineCanSwim {
		t.Fatalf("AC CanSwim bit=%08x, want %08x", got, modernSplineCanSwim)
	}
	if got := modernSplineFlags(legacySplineCatmullRom | 0x00080000); got != 0x00000800|0x00001000 {
		t.Fatalf("CastModern catmull+cyclic=%08x", got)
	}
	if got := modernSplineFlags(legacySplineTrajectory); got != modernSplineParabolic {
		t.Fatalf("trajectory=%08x, want Parabolic only", got)
	}
	body, err := appendCreateSplineData(nil, &LegacyMovementSpline{
		Flags: legacySplineCanSwim, ID: 1, FullTime: 1000, Points: [][3]float32{{1, 2, 3}}, End: [3]float32{4, 5, 6},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	flags := binary.LittleEndian.Uint32(body[17:])
	if flags != modernSplineCanSwim {
		t.Fatalf("CanSwim create spline flags=%08x, want %08x", flags, modernSplineCanSwim)
	}
}

func TestAppendCreateSplineDataDropsUnusableMoveBlock(t *testing.T) {
	header := func(spline *LegacyMovementSpline) []byte {
		t.Helper()
		body, err := appendCreateSplineData(nil, spline, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(body) != 17 || body[16] != 0 {
			t.Fatalf("unusable spline must keep header-only layout, got %d bytes %x", len(body), body)
		}
		return body
	}
	header(&LegacyMovementSpline{
		ID: 9, Time: 500, FullTime: 500,
		Points: [][3]float32{{1, 2, 3}, {4, 5, 6}}, End: [3]float32{4, 5, 6},
	})
	header(&LegacyMovementSpline{
		ID: 9, FullTime: 1000,
		Points: [][3]float32{{10, 10, 10}, {10, 10, 10}}, End: [3]float32{10, 10, 10},
	})
	header(&LegacyMovementSpline{
		ID: 9, FullTime: 1000,
		Points: [][3]float32{{float32(math.NaN()), 0, 0}}, End: [3]float32{1, 2, 3},
	})
}
