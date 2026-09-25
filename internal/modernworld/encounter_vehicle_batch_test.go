package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestEncounterCountersAndRefresh(t *testing.T) {
	for _, tc := range []struct {
		kind   uint32
		params []byte
		opcode uint16
		want   []byte
	}{
		{3, []byte{60}, 0x27b3, pveWords(60)}, {4, []byte{8}, 0x27b4, pveWords(8)},
		{5, []byte{8, 255}, 0x27b9, pveWords(8, 255)}, {6, []byte{8}, 0x27b5, pveWords(8)},
	} {
		body := append(pveWords(tc.kind), tc.params...)
		got, err := TranslateEncounterUnit(body, nil)
		if err != nil || got.Opcode != tc.opcode || !bytes.Equal(got.Body, tc.want) {
			t.Fatal(got, err)
		}
		for n := 0; n < len(body); n++ {
			if _, err := ParseEncounterEvent(body[:n]); err == nil {
				t.Fatalf("accepted prefix %d", n)
			}
		}
		if _, err := ParseEncounterEvent(append(body, 0)); err == nil {
			t.Fatal("accepted trailing byte")
		}
	}
	frames := make(EncounterFrames)
	visible := false
	resolve := func(id uint64) (GUID128, bool) { return GUID128{Low: id, High: 2}, visible }
	if out := frames.Apply(EncounterEvent{Kind: 0, GUID: 42, Param1: 3}, resolve); len(out) != 0 {
		t.Fatal("engaged invisible object")
	}
	if out := frames.Apply(EncounterEvent{Kind: 7}, resolve); len(out) != 0 {
		t.Fatal("refreshed invisible object")
	}
	visible = true
	if out := frames.Flush(resolve); len(out) != 1 || out[0].Opcode != 0x27b0 || out[0].Body[len(out[0].Body)-1] != 3 {
		t.Fatal(out)
	}
	if out := frames.Flush(resolve); len(out) != 0 {
		t.Fatal("duplicate engage")
	}
	frames.Hide(42)
	visible = false
	if out := frames.Apply(EncounterEvent{Kind: 2, GUID: 42, Param1: 7}, resolve); len(out) != 0 {
		t.Fatal(out)
	}
	visible = true
	if out := frames.Flush(resolve); len(out) != 1 || out[0].Body[len(out[0].Body)-1] != 7 {
		t.Fatal(out)
	}
	frames.Hide(42)
	visible = false
	if out := frames.Apply(EncounterEvent{Kind: 1, GUID: 42}, resolve); len(out) != 1 || out[0].Opcode != 0x27b1 {
		t.Fatal("disengage must clear last emitted GUID", out)
	}
	visible = true
	if out := frames.Apply(EncounterEvent{Kind: 7}, resolve); len(out) != 0 {
		t.Fatal("revived disengaged boss")
	}
	if e, err := ParseEncounterEvent(pveWords(7)); err != nil || e.Kind != 7 {
		t.Fatal(e, err)
	}
}

func TestVehicleRequestsAndData(t *testing.T) {
	resolve := func(g GUID128) (uint64, bool) { return 0xf150000001000042, g.Low == 42 }
	for i, op := range []uint16{12855, 12856, 12857} {
		legacy, b, handled, err := TranslateVehicleRequest(op, nil, resolve)
		if !handled || err != nil || legacy != uint32(0x476+i) || len(b) != 0 {
			t.Fatal(legacy, b, handled, err)
		}
		if _, _, _, err := TranslateVehicleRequest(op, []byte{0}, resolve); err == nil {
			t.Fatal("nonempty action")
		}
	}
	request := append(appendPackedGUID128(nil, 42, 0), 0xff)
	op, b, handled, err := TranslateVehicleRequest(CMSGRequestVehicleSwitchSeat, request, resolve)
	if err != nil || !handled || op != 0x479 {
		t.Fatal(op, handled, err)
	}
	r := movementReader{data: b}
	g, _ := r.guid64()
	seat, _ := r.u8()
	if g != 0xf150000001000042 || seat != 0xff || r.remaining() != 0 {
		t.Fatal(g, seat)
	}
	if _, _, _, err := TranslateVehicleRequest(CMSGRequestVehicleSwitchSeat, append(request, 0), resolve); err == nil {
		t.Fatal("trailing bytes")
	}
	if _, _, _, err := TranslateVehicleRequest(CMSGRequestVehicleSwitchSeat, []byte{0, 0, 1}, resolve); err == nil {
		t.Fatal("unknown vehicle")
	}
	for _, id := range []uint32{123, 0} {
		body := binary.LittleEndian.AppendUint32(appendLegacyPackedGUID(nil, 42), id)
		out, err := TranslatePlayerVehicleData(body, func(uint64) GUID128 { return GUID128{Low: 42, High: 5} })
		if err != nil {
			t.Fatal(err)
		}
		r := movementReader{data: out}
		guid, _ := r.guid128()
		got, _ := r.u32()
		if guid != (GUID128{Low: 42, High: 5}) || got != id || r.remaining() != 0 {
			t.Fatal(guid, got)
		}
	}
}

func TestVehiclePassengerAndControlledSeatRequests(t *testing.T) {
	guid := GUID128{Low: 42, High: 99}
	resolve := func(g GUID128) (uint64, bool) { return 42, g == guid }
	for i, op := range []uint16{CMSGRideVehicleInteract, CMSGEjectPassenger} {
		body := appendPackedGUID128(nil, guid.Low, guid.High)
		legacy, got, handled, err := TranslateVehicleRequest(op, body, resolve)
		if err != nil || !handled || legacy != uint32(0x4a8+i) || len(got) != 8 || binary.LittleEndian.Uint64(got) != 42 {
			t.Fatal(legacy, got, handled, err)
		}
		if _, _, _, err := TranslateVehicleRequest(op, append(body, 0), resolve); err == nil {
			t.Fatal("accepted trailing byte")
		}
	}
	move := LegacyMovement{MoveTime: 100, X: 1, Y: 2, Z: 3, TransportSeat: -1}
	body := EncodeMoveUpdate(move, guid, GUID128{})
	body = append(append(body, appendPackedGUID128(nil, guid.Low, guid.High)...), 0xff)
	legacy, got, handled, err := TranslateVehicleRequest(CMSGMoveChangeVehicleSeats, body, resolve)
	if err != nil || !handled || legacy != 0x49b {
		t.Fatal(legacy, handled, err)
	}
	move.MoveExtra = 0x200 // Existing modern serializer supplies the legacy proxy extra word.
	want, err := EncodeLegacyPlayerMovement(PlayerMovement{Mover: guid, Move: move}, 42, 0)
	if err != nil {
		t.Fatal(err)
	}
	want = append(appendLegacyPackedGUID(want, 42), 0xff)
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x want %x", got, want)
	}
	for n := 0; n < len(body); n++ {
		if _, _, _, err := TranslateVehicleRequest(CMSGMoveChangeVehicleSeats, body[:n], resolve); err == nil {
			t.Fatalf("accepted prefix %d", n)
		}
	}
}

func TestStandaloneMovementUsesCachedIdentities(t *testing.T) {
	resolve := func(id uint64) GUID128 {
		if id == 0 {
			return GUID128{}
		}
		return GUID128{Low: id, High: 99}
	}
	m := &LegacyMovement{UpdateFlags: legacyUpdateLiving | legacyUpdateVehicle, MoveFlags: legacyMoveOnTransport,
		X: 1, Y: 2, Z: 3, Orientation: 1, TransportGUID: 77, TransportX: 4, TransportY: 5, TransportZ: 6, TransportSeat: 2, VehicleID: 88,
		WalkSpeed: 2, RunSpeed: 7, RunBackSpeed: 4, SwimSpeed: 5, SwimBackSpeed: 3, FlightSpeed: 9, FlightBackSpeed: 6, TurnRate: 3, PitchRate: 2,
		Spline: &LegacyMovementSpline{Flags: legacySplineFinalTarget, Time: 500, FullTime: 2000, ID: 123, FacingTarget: 43, Points: [][3]float32{{-1, 0, 0}, {0, 0, 0}, {10, 0, 0}, {11, 0, 0}}}}
	out, err := EncodeObjectMovement(LegacyObjectUpdate{Type: LegacyUpdateMovement, GUID: 42, Movement: m}, 3, resolve)
	if err != nil || len(out) != 12 {
		t.Fatal(len(out), err)
	}
	move, err := ParseModernPlayerMovement(out[0].Body)
	if err != nil || move.Mover != resolve(42) || move.Transport != resolve(77) || move.Move.TransportSeat != 2 || move.Move.X != 1 {
		t.Fatal(move, err)
	}
	last := out[len(out)-1]
	if last.Opcode != SMSGOnMonsterMove {
		t.Fatal(last)
	}
	r := movementReader{data: last.Body}
	_, _ = r.guid128()
	_, _ = r.float32s(3)
	id, _ := r.u32()
	_, _ = r.float32s(3)
	_, _ = r.u8()
	flags, _ := r.u32()
	elapsed, _ := r.u32()
	duration, _ := r.u32()
	if id != 123 || elapsed != 500 || duration != 2000 || flags&modernSplineUncompressedPath == 0 {
		t.Fatal(id, elapsed, duration, flags)
	}
	if _, err := EncodeObjectMovement(LegacyObjectUpdate{Type: LegacyUpdateMovement, GUID: 42, Movement: m}, 5, resolve); err == nil {
		t.Fatal("GO must not use Unit movement packet")
	}
}
