package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGRequestVehicleExit              = uint16(12855)
	CMSGRequestVehiclePrevSeat          = uint16(12856)
	CMSGRequestVehicleNextSeat          = uint16(12857)
	CMSGRequestVehicleSwitchSeat        = uint16(12858)
	CMSGRideVehicleInteract             = uint16(0x323B)
	CMSGEjectPassenger                  = uint16(0x323C)
	CMSGMoveChangeVehicleSeats          = uint16(0x3A34)
	SMSGOnCancelExpectedRideVehicleAura = uint16(9958)
	SMSGSetVehicleRecID                 = uint16(0x26F7)
	LegacySMSGPlayerVehicleData         = uint16(0x4A7)
	LegacySMSGCancelVehicleAura         = uint16(0x49D)
)

// Seat validity and occupancy remain authoritative on the legacy server.
func TranslateVehicleRequest(opcode uint16, body []byte, resolve func(GUID128) (uint64, bool)) (uint32, []byte, bool, error) {
	switch opcode {
	case CMSGRideVehicleInteract, CMSGEjectPassenger:
		guid, err := ParseSetActiveMover(body)
		if err != nil {
			return 0, nil, true, err
		}
		legacy, ok := resolve(guid)
		if !ok || legacy == 0 {
			return 0, nil, true, fmt.Errorf("vehicle request references unknown target")
		}
		legacyOpcode := uint32(0x4A8)
		if opcode == CMSGEjectPassenger {
			legacyOpcode = 0x4A9
		}
		return legacyOpcode, binary.LittleEndian.AppendUint64(nil, legacy), true, nil
	case CMSGMoveChangeVehicleSeats:
		r := movementReader{data: body}
		move, err := r.modernMovementStats()
		if err != nil {
			return 0, nil, true, err
		}
		dest, err := r.guid128()
		if err != nil {
			return 0, nil, true, err
		}
		seat, err := r.u8()
		if err != nil {
			return 0, nil, true, err
		}
		if r.remaining() != 0 {
			return 0, nil, true, fmt.Errorf("vehicle seat movement has trailing bytes")
		}
		mover, moverOK := resolve(move.Mover)
		transport, transportOK := resolve(move.Transport)
		destination, destOK := resolve(dest)
		if move.Transport == (GUID128{}) {
			transport = 0
		}
		if dest == (GUID128{}) {
			destination = 0
		}
		if !moverOK || mover == 0 || !transportOK && move.Transport != (GUID128{}) || !destOK && dest != (GUID128{}) {
			return 0, nil, true, fmt.Errorf("vehicle seat movement references unknown object")
		}
		encoded, err := EncodeLegacyPlayerMovement(move, mover, transport)
		if err != nil {
			return 0, nil, true, err
		}
		return 0x49B, append(appendLegacyPackedGUID(encoded, destination), seat), true, nil
	case CMSGRequestVehicleExit, CMSGRequestVehiclePrevSeat, CMSGRequestVehicleNextSeat:
		if len(body) != 0 {
			return 0, nil, true, fmt.Errorf("vehicle action has %d bytes, want 0", len(body))
		}
		return uint32(0x476 + opcode - CMSGRequestVehicleExit), nil, true, nil
	case CMSGRequestVehicleSwitchSeat:
		r := movementReader{data: body}
		guid, err := r.guid128()
		if err != nil {
			return 0, nil, true, err
		}
		seat, err := r.u8()
		if err != nil {
			return 0, nil, true, err
		}
		if r.remaining() != 0 {
			return 0, nil, true, fmt.Errorf("vehicle switch has trailing bytes")
		}
		legacy, ok := resolve(guid)
		if !ok || legacy == 0 {
			return 0, nil, true, fmt.Errorf("vehicle switch references an unknown vehicle")
		}
		return 0x479, append(appendLegacyPackedGUID(nil, legacy), seat), true, nil
	}
	return 0, nil, false, nil
}

// AC SMSG_PLAYER_VEHICLE_DATA and modern SMSG_SET_VEHICLE_REC_ID are semantic
// equivalents despite the different names: packed GUID followed by uint32.
func TranslatePlayerVehicleData(body []byte, resolve func(uint64) GUID128) ([]byte, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return nil, err
	}
	id, err := r.u32()
	if err != nil {
		return nil, err
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("vehicle data has trailing bytes")
	}
	modern := resolve(guid)
	return binary.LittleEndian.AppendUint32(appendPackedGUID128(nil, modern.Low, modern.High), id), nil
}
