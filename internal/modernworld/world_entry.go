package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	SMSGAuraUpdate             = uint16(11295)
	SMSGInitWorldStates        = uint16(10054)
	SMSGUpdateWorldState       = uint16(10056)
	SMSGPhaseShiftChange       = uint16(9592)
	CMSGMoveInitActiveComplete = uint16(14918)
)

type WorldState struct {
	Variable uint32
	Value    int32
}

type InitWorldStates struct {
	MapID  uint32
	ZoneID uint32
	AreaID uint32
	States []WorldState
}

// TranslateInitWorldStates converts WotLK's uint16 state count into the
// build-54261 int32 layout and fills the global Classic UI feature states which
// a 3.4.3 client normally receives from a native server.
func TranslateInitWorldStates(legacy []byte) (InitWorldStates, []byte, error) {
	if len(legacy) < 14 {
		return InitWorldStates{}, nil, fmt.Errorf("legacy init-world-states has %d bytes, want at least 14", len(legacy))
	}
	result := InitWorldStates{
		MapID:  binary.LittleEndian.Uint32(legacy),
		ZoneID: binary.LittleEndian.Uint32(legacy[4:]),
		AreaID: binary.LittleEndian.Uint32(legacy[8:]),
	}
	count := int(binary.LittleEndian.Uint16(legacy[12:]))
	if count > 4096 {
		return InitWorldStates{}, nil, fmt.Errorf("legacy init-world-states count %d exceeds 4096", count)
	}
	if expected := 14 + count*8; len(legacy) != expected {
		return InitWorldStates{}, nil, fmt.Errorf("legacy init-world-states has %d bytes, count requires %d", len(legacy), expected)
	}
	seen := make(map[uint32]struct{}, count+len(modernWorldStateDefaults))
	result.States = make([]WorldState, 0, count+len(modernWorldStateDefaults))
	position := 14
	for index := 0; index < count; index++ {
		state := WorldState{
			Variable: binary.LittleEndian.Uint32(legacy[position:]),
			Value:    int32(binary.LittleEndian.Uint32(legacy[position+4:])),
		}
		position += 8
		if state.Variable == 0 && state.Value == 0 {
			continue
		}
		result.States = append(result.States, state)
		seen[state.Variable] = struct{}{}
	}
	for _, state := range modernWorldStateDefaults {
		if _, exists := seen[state.Variable]; exists {
			continue
		}
		result.States = append(result.States, state)
	}
	return result, EncodeInitWorldStates(result), nil
}

func EncodeInitWorldStates(states InitWorldStates) []byte {
	body := make([]byte, 0, 16+len(states.States)*8)
	body = binary.LittleEndian.AppendUint32(body, states.MapID)
	body = binary.LittleEndian.AppendUint32(body, states.ZoneID)
	body = binary.LittleEndian.AppendUint32(body, states.AreaID)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(states.States)))
	for _, state := range states.States {
		body = binary.LittleEndian.AppendUint32(body, state.Variable)
		body = binary.LittleEndian.AppendUint32(body, uint32(state.Value))
	}
	return body
}

func TranslateUpdateWorldState(legacy []byte) ([]byte, error) {
	if len(legacy) != 8 && len(legacy) != 9 {
		return nil, fmt.Errorf("legacy update-world-state has %d bytes, want 8 or 9", len(legacy))
	}
	hidden := len(legacy) == 9 && legacy[8] != 0
	bits := newBitWriter(append([]byte(nil), legacy[:8]...))
	bits.writeBit(hidden)
	return bits.flush(), nil
}

func EncodeEmptyUpdateObject(mapID uint16) []byte {
	body := binary.LittleEndian.AppendUint32(nil, 0)
	body = binary.LittleEndian.AppendUint16(body, mapID)
	bits := newBitWriter(body)
	bits.writeBit(false)
	body = bits.flush()
	return binary.LittleEndian.AppendUint32(body, 0)
}

func EncodeDefaultPhaseShift(playerGUID uint64) []byte {
	return EncodePhaseShiftFromLegacyMask(0, playerGUID)
}

// EncodePhaseShiftFromLegacyMask translates WotLK's SMSG_PHASE_SHIFT_CHANGE
// uint32 mask into the 54261 phase list. Only masks 16 and 32 have verified
// Phase.db2 counterparts in this client build (173/174, the two factories).
func EncodePhaseShiftFromLegacyMask(mask, playerGUID uint64) []byte {
	low, high := modernPlayerGUID(playerGUID)
	body := appendPackedGUID128(nil, low, high)
	phases := make([]uint16, 0, 2)
	if mask&16 != 0 {
		phases = append(phases, 173)
	}
	if mask&32 != 0 {
		phases = append(phases, 174)
	}
	phaseFlags := uint32(8) // unphased
	if len(phases) != 0 {
		phaseFlags = 0
	}
	body = binary.LittleEndian.AppendUint32(body, phaseFlags)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(phases)))
	body = appendPackedGUID128(body, 0, 0) // personal GUID
	for _, phase := range phases {
		body = binary.LittleEndian.AppendUint16(body, 1) // phase flags
		body = binary.LittleEndian.AppendUint16(body, phase)
	}
	body = binary.LittleEndian.AppendUint32(body, 0) // visible maps
	body = binary.LittleEndian.AppendUint32(body, 0) // preload maps
	body = binary.LittleEndian.AppendUint32(body, 0) // UI map phases
	return body
}

func TranslateLegacyPhaseShiftChange(body []byte, playerGUID uint64) ([]byte, error) {
	if len(body) != 4 {
		return nil, fmt.Errorf("phase-shift change has %d bytes, want 4", len(body))
	}
	return EncodePhaseShiftFromLegacyMask(uint64(binary.LittleEndian.Uint32(body)), playerGUID), nil
}

func ParseInitActiveMoverComplete(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("init-active-mover-complete has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

var modernWorldStateDefaults = []WorldState{
	{17223, 1}, {17647, 1}, {17648, 1}, {20445, 0}, {20446, 0}, {20447, 1},
	{20487, 1}, {20488, 1}, {20489, 1}, {20491, 1}, {20492, 1}, {20493, 1},
	{20494, 0}, {20495, 0}, {20496, 0}, {20497, 0}, {20518, 0}, {20560, 0},
	{20562, 1}, {20563, 1}, {20567, 0}, {20738, 0}, {20882, 0}, {21125, 1},
	{21126, 1}, {21195, 2725}, {21196, 2542}, {21197, 2203}, {21198, 1898}, {21199, 1453},
	{21200, 2548}, {21201, 2391}, {21202, 2086}, {21203, 1777}, {21204, 1431}, {21205, 2354},
	{21206, 2181}, {21207, 1922}, {21208, 1686}, {21209, 1408}, {21238, 2},
	{17224, 1}, {17225, 1}, {17227, 1}, {21975, 1},
}
