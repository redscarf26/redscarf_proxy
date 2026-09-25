package modernworld

import (
	"encoding/hex"
	"math"
	"testing"
)

func TestDumpCannonCreate(t *testing.T) {
	f := math.Float32frombits
	move := &LegacyMovement{
		UpdateFlags:          legacyUpdateLiving | legacyUpdateVehicle,
		MoveFlags:            legacyMoveOnTransport,
		MoveExtra:            0x3b,
		MoveTime:             0x61236,
		X:                    f(0xc3e1ea4e),
		Y:                    f(0x451bb0cd),
		Z:                    f(0x433f91bb),
		Orientation:          f(0x3fc360ac),
		TransportGUID:        0x1fc0000000000015,
		TransportX:           f(0xc0c4fc7a),
		TransportY:           f(0xc1c9e8dc),
		TransportZ:           f(0x41ada3d7),
		TransportOrientation: f(0x4096cbe6),
		TransportSeat:        -1,
		VehicleID:            554,
		VehicleOrientation:   f(0x4096cbe6),
		WalkSpeed:            2.5,
		RunSpeed:             7,
		RunBackSpeed:         4.5,
		SwimSpeed:            f(0x40971c71),
		SwimBackSpeed:        2.5,
		FlightSpeed:          7,
		FlightBackSpeed:      4.5,
		TurnRate:             f(0x40490fe0),
		PitchRate:            f(0x4048f5c3),
	}
	update := LegacyObjectUpdate{
		Type: LegacyUpdateCreateObject1, GUID: 0xf150008fe600023f, ObjectType: 3,
		Movement: move,
		Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{
			legacyObjectEntry:         36838,
			legacyObjectScale:         math.Float32bits(1),
			legacyUnitHealth:          350000,
			legacyUnitMaxHealth:       350000,
			legacyUnitLevel:           83,
			legacyUnitFaction: 1771,
			legacyUnitNPCFlags:        0x01000000,
			legacyUnitDisplayID:       29488,
			legacyUnitNativeDisplayID: 29488,
			legacyUnitFlags:           0x00000008,
			legacyUnitBytes0:          0x00000001,
		}},
	}
	body, err := EncodeUnitCreate(update, ActivePlayerCreateOptions{MapID: 631})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ours values block from 228: %s", hex.EncodeToString(body[11+228:]))
}
