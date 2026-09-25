package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

// EncodeObjectMovement maps legacy movement-only updates to modern movement
// messages. Modern UpdateObject has no Movement record type. Non-unit world
// transforms require a separately verified client path and must not be sent to
// the Unit movement handler or implemented by destroying/recreating passengers.
func EncodeObjectMovement(update LegacyObjectUpdate, objectType uint8, resolve func(uint64) GUID128) ([]Packet, error) {
	if update.Type != LegacyUpdateMovement || update.Movement == nil {
		return nil, fmt.Errorf("expected standalone movement")
	}
	if objectType != 3 && objectType != 4 {
		return nil, fmt.Errorf("standalone world transform for object type %d is not mapped", objectType)
	}
	m := *update.Movement
	if m.UpdateFlags&(legacyUpdateLiving|legacyUpdateStationary|legacyUpdateGOPosition) == 0 {
		return nil, fmt.Errorf("movement has no unit position")
	}
	guid, transport := resolve(update.GUID), resolve(m.TransportGUID)
	if guid == (GUID128{}) {
		return nil, fmt.Errorf("unknown movement GUID")
	}
	packets := []Packet{{Opcode: SMSGMoveUpdate, Body: EncodeMoveUpdate(m, guid, transport)}}
	if m.UpdateFlags&legacyUpdateLiving != 0 {
		for _, speed := range []struct {
			opcode uint16
			value  float32
		}{
			{SMSGMoveSplineSetWalkSpeed, m.WalkSpeed}, {SMSGMoveSplineSetRunSpeed, m.RunSpeed},
			{SMSGMoveSplineSetRunBackSpeed, m.RunBackSpeed}, {SMSGMoveSplineSetSwimSpeed, m.SwimSpeed},
			{SMSGMoveSplineSetSwimBackSpeed, m.SwimBackSpeed}, {SMSGMoveSplineSetFlightSpeed, m.FlightSpeed},
			{SMSGMoveSplineSetFlightBackSpeed, m.FlightBackSpeed}, {SMSGMoveSplineSetTurnRate, m.TurnRate}, {SMSGMoveSplineSetPitchRate, m.PitchRate},
		} {
			packets = append(packets, Packet{Opcode: speed.opcode, Body: appendFloat32(appendPackedGUID128(nil, guid.Low, guid.High), speed.value)})
		}
	}
	if m.UpdateFlags&legacyUpdateVehicle != 0 {
		packets = append(packets, Packet{Opcode: SMSGSetVehicleRecID, Body: binary.LittleEndian.AppendUint32(appendPackedGUID128(nil, guid.Low, guid.High), m.VehicleID)})
	}
	if sp := m.Spline; sp != nil {
		move := LegacyMonsterMove{MoverGUID: update.GUID, TransportGUID: m.TransportGUID, TransportSeat: m.TransportSeat,
			StartX: m.X, StartY: m.Y, StartZ: m.Z, SplineID: sp.ID, Flags: sp.Flags, Duration: sp.FullTime, Elapsed: sp.Time, Mode: sp.Mode,
			FacingTarget: sp.FacingTarget, FacingAngle: sp.FacingAngle, FacingX: sp.FacingX, FacingY: sp.FacingY, FacingZ: sp.FacingZ}
		if sp.Flags&legacySplineTrajectory != 0 {
			move.JumpGravity = math.Float32frombits(uint32(sp.VerticalAccel))
			move.JumpStartTime = uint32(sp.StartTime)
		}
		if sp.FullTime <= sp.Time || len(sp.Points) < 4 {
			move.SplineType = legacySplineTypeStop
		} else {
			// AC WriteCreate contains the padded control array. WriteMonsterMove
			// starts at point 1 and writes points [2, count-1), omitting padding.
			for _, point := range sp.Points {
				if !finiteVec3(point) {
					return nil, fmt.Errorf("non-finite spline point")
				}
			}
			move.StartX, move.StartY, move.StartZ = sp.Points[1][0], sp.Points[1][1], sp.Points[1][2]
			move.Points = append([][3]float32(nil), sp.Points[2:len(sp.Points)-1]...)
			move.Uncompressed = true
			if sp.Flags&0x80000 != 0 { // cyclic, matching AC PacketBuilder
				if sp.Flags&legacySplineFlying != 0 {
					move.Points = append([][3]float32{sp.Points[1]}, move.Points...)
					move.Flags |= 0x100000
				} else {
					move.Points = append(move.Points, [3]float32{})
				}
			}
			switch {
			case sp.Flags&legacySplineFinalTarget != 0:
				move.SplineType = legacySplineTypeFacingTarget
			case sp.Flags&legacySplineFinalOrientation != 0:
				move.SplineType = legacySplineTypeFacingAngle
			case sp.Flags&legacySplineFinalPoint != 0:
				move.SplineType = legacySplineTypeFacingSpot
			}
		}
		body, err := EncodeMonsterMove(move, guid, transport, resolve(sp.FacingTarget))
		if err != nil {
			return nil, err
		}
		packets = append(packets, Packet{Opcode: SMSGOnMonsterMove, Body: body})
	}
	return packets, nil
}
