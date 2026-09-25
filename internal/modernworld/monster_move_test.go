package modernworld

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestParseLegacyMonsterMoveCompressedFacingTarget(t *testing.T) {
	body := appendTestPackedGUID(nil, 0xf130000001000043)
	body = append(body, 0) // Toggle AnimTierInTrans
	for _, value := range []float32{100, 200, 300} {
		body = appendFloat32(body, value)
	}
	body = binary.LittleEndian.AppendUint32(body, 77)
	body = append(body, legacySplineTypeFacingTarget)
	body = binary.LittleEndian.AppendUint64(body, 0x44)
	body = binary.LittleEndian.AppendUint32(body, legacySplineAnimationTier|legacySplineTrajectory)
	body = append(body, 3)
	body = binary.LittleEndian.AppendUint32(body, 25)
	body = binary.LittleEndian.AppendUint32(body, 1500)
	body = appendFloat32(body, 9.5)
	body = binary.LittleEndian.AppendUint32(body, 125)
	body = binary.LittleEndian.AppendUint32(body, 3)
	for _, value := range []float32{110, 210, 310} {
		body = appendFloat32(body, value)
	}
	body = binary.LittleEndian.AppendUint32(body, 0x00100200)
	body = binary.LittleEndian.AppendUint32(body, 0xffe00c00)

	move, err := ParseLegacyMonsterMove(body, false)
	if err != nil {
		t.Fatal(err)
	}
	if move.MoverGUID != 0xf130000001000043 || move.FacingTarget != 0x44 || move.SplineID != 77 || move.Duration != 1500 {
		t.Fatalf("unexpected parsed move: %#v", move)
	}
	if move.TransportSeat != 0 {
		t.Fatalf("normal monster-move transport seat=%d, want legacy proxy default 0", move.TransportSeat)
	}
	if !move.HasEnd || move.End != [3]float32{110, 210, 310} || len(move.PackedDeltas) != 2 || move.PackedDeltas[1] != 0xffe00c00 {
		t.Fatalf("unexpected compressed path: end=%v deltas=%x", move.End, move.PackedDeltas)
	}
	if move.JumpGravity != 9.5 || move.JumpStartTime != 125 {
		t.Fatalf("trajectory payload gravity=%g start=%d", move.JumpGravity, move.JumpStartTime)
	}
}

func TestParseLegacyMonsterMoveTransportAndUncompressed(t *testing.T) {
	body := appendTestPackedGUID(nil, 0x42)
	body = appendTestPackedGUID(body, 0xf120000001000099)
	body = append(body, 2, 0) // seat, toggle
	for _, value := range []float32{1, 2, 3} {
		body = appendFloat32(body, value)
	}
	body = binary.LittleEndian.AppendUint32(body, 5)
	body = append(body, legacySplineTypeFacingSpot)
	for _, value := range []float32{4, 5, 6} {
		body = appendFloat32(body, value)
	}
	body = binary.LittleEndian.AppendUint32(body, legacySplineFlying|legacySplineCatmullRom)
	body = binary.LittleEndian.AppendUint32(body, 900)
	body = binary.LittleEndian.AppendUint32(body, 2)
	for _, value := range []float32{7, 8, 9, 10, 11, 12} {
		body = appendFloat32(body, value)
	}

	move, err := ParseLegacyMonsterMove(body, true)
	if err != nil {
		t.Fatal(err)
	}
	if move.TransportGUID != 0xf120000001000099 || move.TransportSeat != 2 || len(move.Points) != 2 || move.Points[1][2] != 12 {
		t.Fatalf("unexpected transport move: %#v", move)
	}
}

func TestEncodeMonsterMoveBuild54261Layout(t *testing.T) {
	move := LegacyMonsterMove{
		StartX: 100, StartY: 200, StartZ: 300,
		SplineID: 7, SplineType: legacySplineTypeFacingAngle,
		Flags:    legacySplineCatmullRom | 0x00080000 | 0x20000000,
		Duration: 1234, FacingAngle: -0.5,
		Points:        [][3]float32{{101, 201, 301}, {102, 202, 302}},
		TransportSeat: 0,
	}
	body, err := EncodeMonsterMove(move, GUID128{Low: 0x42}, GUID128{}, GUID128{})
	if err != nil {
		t.Fatal(err)
	}
	// Packed mover (01 00 42), start, spline id, zero destination, then four bits.
	if len(body) != 3+12+4+12+1+4+4+4+4+1+2+1+5+4+24 {
		t.Fatalf("unexpected body size %d: %x", len(body), body)
	}
	if body[0] != 1 || body[1] != 0 || body[2] != 0x42 || body[31] != 0 {
		t.Fatalf("unexpected fixed prefix: %x", body[:32])
	}
	flags := binary.LittleEndian.Uint32(body[32:36])
	if want := uint32(0x00400000 | 0x00000800 | 0x00001000 | 0x02000000); flags != want {
		t.Fatalf("modern flags=%08x want=%08x", flags, want)
	}
	if binary.LittleEndian.Uint32(body[40:44]) != 1234 || body[48] != 0 || body[51] != 0 {
		t.Fatalf("unexpected timing/transport fields: %x", body[36:52])
	}
	// FacingAngle is clamped into [0, 2pi), followed by two uncompressed points.
	angle := math.Float32frombits(binary.LittleEndian.Uint32(body[57:61]))
	if math.Abs(float64(angle-(2*math.Pi-0.5))) > 0.0001 {
		t.Fatalf("facing angle=%f", angle)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(body[61:65])); got != 101 {
		t.Fatalf("first point X=%f", got)
	}
}

func TestEncodeMonsterMoveWritesJumpExtraFromTrajectory(t *testing.T) {
	move := LegacyMonsterMove{
		StartX: 0, StartY: 0, StartZ: 20,
		SplineID: 9, Duration: 800, TransportSeat: 0,
		Flags:         legacySplineTrajectory,
		JumpGravity:   19.2,
		JumpStartTime: 40,
		End:           [3]float32{8, 8, 20},
		HasEnd:        true,
		PackedDeltas:  []uint32{8 | 4<<11},
	}
	body, err := EncodeMonsterMove(move, GUID128{Low: 0x42}, GUID128{}, GUID128{})
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: body[52:]}
	_, _ = r.bits(2)
	pointCount, _ := r.bits(16)
	_, _ = r.bits(1)
	_, _ = r.bits(1)
	packedCount, _ := r.bits(16)
	filter, _ := r.bit()
	spellExtra, _ := r.bit()
	jumpExtra, _ := r.bit()
	if pointCount != 1 || packedCount != 1 || filter || spellExtra || !jumpExtra {
		t.Fatalf("points=%d packed=%d filter=%v spell=%v jump=%v", pointCount, packedCount, filter, spellExtra, jumpExtra)
	}
	r.align()
	for _, want := range []float32{8, 8, 20} {
		got, readErr := r.f32()
		if readErr != nil || got != want {
			t.Fatalf("path coordinate=%f want=%f err=%v", got, want, readErr)
		}
	}
	if packed, readErr := r.u32(); readErr != nil || packed != 8|4<<11 {
		t.Fatalf("packed delta read err=%v", readErr)
	}
	gravity, err := r.f32()
	if err != nil || gravity != 19.2 {
		t.Fatalf("jump gravity=%f err=%v", gravity, err)
	}
	start, err := r.u32()
	if err != nil || start != 40 {
		t.Fatalf("jump start time=%d err=%v", start, err)
	}
	if r.remaining() != 0 {
		t.Fatalf("trajectory jump has %d trailing bytes", r.remaining())
	}
	flags := binary.LittleEndian.Uint32(body[32:36])
	if flags&modernSplineParabolic == 0 {
		t.Fatalf("trajectory flags=%08x missing Parabolic", flags)
	}
	if flags&modernSplineFlying != 0 {
		t.Fatalf("trajectory flags=%08x unexpectedly set Flying", flags)
	}
	if flags&modernSplineCatmullRom != 0 {
		t.Fatalf("trajectory flags=%08x unexpectedly set CatmullRom", flags)
	}
}

func TestGunshipRocketJumpLengthensCloseHops(t *testing.T) {
	closeHop := LegacyMonsterMove{
		Flags: legacySplineTrajectory, JumpGravity: 863.2757,
		Duration: 46, TransportGUID: 0x1fc0000000000016,
	}
	duration, gravity := closeHop.gunshipRocketJump()
	if duration != gunshipJumpMinDurationMS {
		t.Fatalf("close hop duration=%d, want %d", duration, gunshipJumpMinDurationMS)
	}
	seconds := float32(duration) / 1000
	apex := gravity * seconds * seconds / 8
	if math.Abs(float64(apex)-float64(gunshipJumpMinApexYards)) > 0.05 {
		t.Fatalf("close hop apex=%g, want %g", apex, gunshipJumpMinApexYards)
	}

	body, err := EncodeMonsterMove(closeHop, GUID128{Low: 0x12}, GUID128{High: 0x1fc0, Low: 0x16}, GUID128{})
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(body[40:44]); got != gunshipJumpMinDurationMS {
		t.Fatalf("encoded close hop duration=%d, want %d", got, gunshipJumpMinDurationMS)
	}

	ground := closeHop
	ground.TransportGUID = 0
	duration, gravity = ground.gunshipRocketJump()
	if duration != 46 || gravity != 863.2757 {
		t.Fatalf("ground hop duration=%d gravity=%g, want original 46/863", duration, gravity)
	}

	longHop := LegacyMonsterMove{
		Flags: legacySplineTrajectory, JumpGravity: 17.5692,
		Duration: 2278, TransportGUID: 0x1fc0000000000016,
	}
	duration, gravity = longHop.gunshipRocketJump()
	if duration != 2278 {
		t.Fatalf("long hop duration=%d, want unchanged 2278", duration)
	}
	seconds = float32(duration) / 1000
	if apex := gravity * seconds * seconds / 8; apex < gunshipJumpMinApexYards-0.05 {
		t.Fatalf("long hop apex=%g, want at least %g", apex, gunshipJumpMinApexYards)
	}
}

func TestEncodeMonsterMoveKeepsCompressedGroundPath(t *testing.T) {
	move := LegacyMonsterMove{
		StartX: 0, StartY: 0, StartZ: 0,
		SplineID: 8, Duration: 1200, TransportSeat: 0,
		End: [3]float32{8, 8, 8}, HasEnd: true,
		// Midpoint is (4,4,4). legacy proxy keeps this packed offset on the
		// modern wire instead of changing the spline representation.
		PackedDeltas: []uint32{8 | 4<<11},
	}
	body, err := EncodeMonsterMove(move, GUID128{Low: 0x42}, GUID128{}, GUID128{})
	if err != nil {
		t.Fatal(err)
	}
	if flags := binary.LittleEndian.Uint32(body[32:36]); flags&modernSplineUncompressedPath != 0 {
		t.Fatalf("ground spline flags=%#x unexpectedly set uncompressed-path", flags)
	}
	r := movementReader{data: body[52:]}
	typeID, _ := r.bits(2)
	pointCount, _ := r.bits(16)
	_, _ = r.bits(1) // voluntary vehicle exit
	_, _ = r.bits(1) // interpolate
	packedCount, _ := r.bits(16)
	_, _ = r.bits(3)
	if typeID != 0 || pointCount != 1 || packedCount != 1 {
		t.Fatalf("type=%d points=%d packed=%d", typeID, pointCount, packedCount)
	}
	r.align()
	for _, want := range []float32{8, 8, 8} {
		got, readErr := r.f32()
		if readErr != nil || got != want {
			t.Fatalf("path coordinate=%f want=%f err=%v", got, want, readErr)
		}
	}
	packed, readErr := r.u32()
	if readErr != nil || packed != 8|4<<11 {
		t.Fatalf("packed delta=%#x err=%v", packed, readErr)
	}
	if r.remaining() != 0 {
		t.Fatalf("compressed ground spline has %d trailing bytes", r.remaining())
	}
}

func TestEncodeMonsterMoveStopAndFacingTarget(t *testing.T) {
	stop := LegacyMonsterMove{MoverGUID: 0x42, StartX: 1, StartY: 2, StartZ: 3, SplineID: 9, SplineType: legacySplineTypeStop, TransportSeat: 0}
	body, err := EncodeMonsterMove(stop, GUID128{Low: 0x42}, GUID128{}, GUID128{})
	if err != nil {
		t.Fatal(err)
	}
	if body[31] != 0x20 { // StopDistanceTolerance=2 in the high-nibble bit stream.
		t.Fatalf("stop tolerance byte=%02x", body[31])
	}

	target := stop
	target.SplineType = legacySplineTypeFacingTarget
	target.End = [3]float32{1, 2, 3}
	target.HasEnd = true
	target.FacingTarget = 0x99
	player := ModernGUIDForLegacy(0x99, 571)
	body, err = EncodeMonsterMove(target, GUID128{Low: 0x42}, GUID128{}, player)
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: body[52:]}
	typeID, _ := r.bits(2)
	pointCount, _ := r.bits(16)
	_, _ = r.bits(1) // voluntary vehicle exit
	_, _ = r.bits(1) // interpolate
	packedCount, _ := r.bits(16)
	_, _ = r.bits(3)
	if typeID != 2 || pointCount != 1 || packedCount != 0 {
		t.Fatalf("facing target type=%d points=%d packed=%d", typeID, pointCount, packedCount)
	}
	r.align()
	direction, err := r.f32()
	if err != nil || direction != 0 {
		t.Fatalf("FaceDirection=%f err=%v", direction, err)
	}
	low, high, guidBytes, err := readPackedGUID128(r.data[r.offset:])
	if err != nil || low != player.Low || high != player.High {
		t.Fatalf("FaceGUID=%016x:%016x want=%016x:%016x err=%v", high, low, player.High, player.Low, err)
	}
	r.offset += guidBytes
	for _, want := range target.End {
		got, readErr := r.f32()
		if readErr != nil || got != want {
			t.Fatalf("facing-target endpoint=%f want=%f err=%v", got, want, readErr)
		}
	}
	if r.remaining() != 0 {
		t.Fatalf("facing-target packet has %d trailing bytes: %x", r.remaining(), r.data[r.offset:])
	}
}

func TestEncodeTaxiMonsterMoveUsesUncompressedDecoratedSpline(t *testing.T) {
	move := LegacyMonsterMove{
		StartX: 0, StartY: 0, StartZ: 10,
		SplineID: 9, Flags: legacySplineCanSwim | legacySplineFlying,
		Duration: 1000, TransportSeat: -1,
		Points:    [][3]float32{{10, 1, 10}, {20, 0, 10}},
		TaxiStart: true,
	}
	body, err := EncodeMonsterMove(move, GUID128{Low: 0x42}, GUID128{}, GUID128{})
	if err != nil {
		t.Fatal(err)
	}
	// Packed mover is three bytes; flags follow start, spline ID,
	// destination and the tolerance byte.
	if flags := binary.LittleEndian.Uint32(body[32:]); flags != modernTaxiSplineFlags {
		t.Fatalf("taxi flags=0x%08x want=0x%08x", flags, modernTaxiSplineFlags)
	}
	// Two uncompressed points: one decoded intermediate plus the end.
	r := movementReader{data: body[52:]}
	_, _ = r.bits(2)
	pointCount, bitErr := r.bits(16)
	if bitErr != nil || pointCount != 2 {
		t.Fatalf("taxi spline point count=%d err=%v body=%x", pointCount, bitErr, body[52:58])
	}
}

func TestEncodeLaterTaxiSegmentKeepsSmoothFlightWithoutRestartingTaxiCamera(t *testing.T) {
	move := LegacyMonsterMove{
		SplineID: 10, Flags: legacySplineCanSwim | legacySplineFlying,
		Duration: 1000, TransportSeat: -1,
		Points: [][3]float32{{10, 1, 10}},
	}
	body, err := EncodeMonsterMove(move, GUID128{Low: 0x42}, GUID128{}, GUID128{})
	if err != nil {
		t.Fatal(err)
	}
	if flags := binary.LittleEndian.Uint32(body[32:]); flags == modernTaxiSplineFlags {
		t.Fatalf("later taxi segment unexpectedly has start flags 0x%08x", flags)
	} else if want := uint32(0x00600a00); flags != want {
		t.Fatalf("later taxi flags=0x%08x want=0x%08x", flags, want)
	}
}

func TestEncodeMonsterMoveKeepsSwimAnimationFromUnitFlags(t *testing.T) {
	if !LegacyUnitIsSwimming(map[int]uint32{legacyUnitFlags: legacyUnitFlagSwimming}) {
		t.Fatal("UNIT_FLAG_SWIMMING was not detected")
	}
	if LegacyUnitIsSwimming(map[int]uint32{legacyUnitFlags: 0x8}) {
		t.Fatal("non-swimming unit flags were treated as swimming")
	}
	move := LegacyMonsterMove{
		SplineID: 7, Duration: 800, TransportSeat: 0, Swimming: true,
		End: [3]float32{4, 5, 6}, HasEnd: true,
	}
	body, err := EncodeMonsterMove(move, GUID128{Low: 0x42}, GUID128{}, GUID128{})
	if err != nil {
		t.Fatal(err)
	}
	flags := binary.LittleEndian.Uint32(body[32:])
	if flags&modernSplineCanSwim == 0 || flags&modernSplineAnimTierSwim == 0 {
		t.Fatalf("swimming monster-move flags=0x%08x missing CanSwim/AnimTierSwim", flags)
	}
}

func TestEncodeLegacyFlyingSplineUsesModernCatmullRomInterpolation(t *testing.T) {
	move := LegacyMonsterMove{
		SplineID: 11, Flags: legacySplineFlying,
		Duration: 1000, TransportSeat: -1,
		Points: [][3]float32{{5, 1, 10}, {10, 0, 10}},
	}
	body, err := EncodeMonsterMove(move, GUID128{Low: 0x42}, GUID128{}, GUID128{})
	if err != nil {
		t.Fatal(err)
	}
	flags := binary.LittleEndian.Uint32(body[32:])
	if flags&modernSplineCatmullRom == 0 {
		t.Fatalf("legacy flying spline flags=0x%08x missing modern Catmull-Rom", flags)
	}
}

func TestParseLegacyMonsterMoveRejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{name: "truncated", body: []byte{1}, want: "mover GUID"},
		{name: "bad type", body: legacyMonsterMoveHeader(9), want: "unsupported legacy spline type"},
		{name: "stop trailing", body: append(legacyMonsterMoveHeader(legacySplineTypeStop), 1), want: "trailing bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseLegacyMonsterMove(test.body, false)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v, want substring %q", err, test.want)
			}
		})
	}
}

func legacyMonsterMoveHeader(splineType uint8) []byte {
	body := appendTestPackedGUID(nil, 0x42)
	body = append(body, 0)
	for range 3 {
		body = appendFloat32(body, 0)
	}
	body = binary.LittleEndian.AppendUint32(body, 1)
	return append(body, splineType)
}
