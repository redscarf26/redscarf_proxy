package modernworld

import (
	"encoding/binary"
	"fmt"
	"time"
)

const (
	// Server -> client rune state packets for build 3.4.3.54261.
	SMSGAddRunePower = uint16(0x26B8)
	SMSGResyncRunes  = uint16(0x2C5C)
	SMSGConvertRune  = uint16(0x2C5D)

	runeCount            = 6
	runeRechargeDuration = 10 * time.Second
	deathKnightClass     = byte(6)

	// Build 54261 SpellPowerData types for the three rune pairs. legacy proxy writes
	// these on every RUNE_LIST SpellGo as RemainingPower so the rune bar can
	// drop a slot when the spell lands.
	powerRuneBlood  byte = 0x15
	powerRuneFrost  byte = 0x16
	powerRuneUnholy byte = 0x17
)

// DefaultDeathKnightRuneState is the login layout for a death knight: blood,
// blood, unholy, unholy, frost, frost, all six runes available.
func DefaultDeathKnightRuneState() LegacyRuneState {
	return LegacyRuneState{
		Types:     [runeCount]byte{0, 0, 2, 2, 1, 1},
		Cooldowns: [runeCount]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		Available: (1 << runeCount) - 1,
	}
}

// RuneStateForCreate returns the ActivePlayer movement rune block. Cached
// SMSG_RESYNC_RUNES wins when present; otherwise a death knight still needs
// the default six-rune mask so the client can light the rune bar before the
// standalone resync packet arrives.
func RuneStateForCreate(class byte, cached *LegacyRuneState) *LegacyRuneState {
	if cached != nil {
		return cached
	}
	if class == deathKnightClass {
		state := DefaultDeathKnightRuneState()
		return &state
	}
	return nil
}

// RuneData is the optional RemainingRunes block carried by SpellCastData.
// Start and Count are the pre/post-cast availability masks used by the client.
type RuneData struct {
	Start     byte
	Count     byte
	Cooldowns []byte
}

// SpellPowerData is one RemainingPower row. Build 54261 writes Cost then Type.
type SpellPowerData struct {
	Cost int32
	Type byte
}

// LegacyRuneState is the state delivered by 3.3.5a SMSG_RESYNC_RUNES.
type LegacyRuneState struct {
	Types     [runeCount]byte
	Cooldowns [runeCount]byte
	Available byte
}

func ParseLegacyRuneResync(body []byte) (LegacyRuneState, error) {
	// legacy proxy initializes both modern availability masks to all six runes and
	// forwards the per-slot cooldown progress bytes. Rune types are consumed
	// for validation only; the 3.4.3 RuneDataReSync payload has no type array.
	state := LegacyRuneState{
		Available: (1 << runeCount) - 1,
		Cooldowns: [runeCount]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	}
	r := movementReader{data: body}
	count, err := r.u32()
	if err != nil {
		return state, fmt.Errorf("read rune count: %w", err)
	}
	if count > runeCount {
		return state, fmt.Errorf("rune count %d exceeds %d", count, runeCount)
	}
	for index := 0; index < int(count); index++ {
		runeType, typeErr := r.u8()
		if typeErr != nil {
			return state, fmt.Errorf("read rune %d type: %w", index, typeErr)
		}
		cooldown, cooldownErr := r.u8()
		if cooldownErr != nil {
			return state, fmt.Errorf("read rune %d cooldown: %w", index, cooldownErr)
		}
		state.Types[index] = runeType
		state.Cooldowns[index] = cooldown
	}
	if r.remaining() != 0 {
		return state, fmt.Errorf("rune resync has %d trailing bytes", r.remaining())
	}
	return state, nil
}

func EncodeRuneResync(state LegacyRuneState) []byte {
	body := []byte{state.Available, state.Available}
	body = binary.LittleEndian.AppendUint32(body, runeCount)
	return append(body, state.Cooldowns[:]...)
}

func ParseLegacyAddRunePower(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("add-rune-power has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func EncodeAddRunePower(power uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, power)
}

func ParseLegacyRuneConversion(body []byte) (slot, runeType byte, err error) {
	if len(body) != 2 {
		return 0, 0, fmt.Errorf("convert-rune has %d bytes, want 2", len(body))
	}
	slot, runeType = body[0], body[1]
	if slot >= runeCount {
		return 0, 0, fmt.Errorf("rune slot %d exceeds %d", slot, runeCount-1)
	}
	if runeType > 3 {
		return 0, 0, fmt.Errorf("rune type %d exceeds 3", runeType)
	}
	return slot, runeType, nil
}

func EncodeRuneConversion(slot, runeType byte) []byte {
	// When a death rune expires, the modern client expects the slot's base
	// type, not the legacy packet's transitional type. Slots are blood,
	// unholy and frost in pairs.
	if runeType != 3 {
		runeType = [...]byte{0, 0, 2, 2, 1, 1}[slot]
	}
	body := make([]byte, 14)
	body[6] = slot
	body[10] = runeType
	return body
}

func EncodeRuneData(data RuneData) []byte {
	body := []byte{data.Start, data.Count}
	body = binary.LittleEndian.AppendUint32(body, uint32(len(data.Cooldowns)))
	return append(body, data.Cooldowns...)
}

func EncodeSpellPowerData(power SpellPowerData) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(power.Cost))
	return append(body, power.Type)
}

// RuneRemainingPower is the three RemainingPower rows legacy proxy emits on a rune
// SpellGo: blood, frost, then unholy counts from the post-cast mask.
func RuneRemainingPower(usable byte) []SpellPowerData {
	var blood, unholy, frost int32
	for index := 0; index < runeCount; index++ {
		if usable&(1<<index) == 0 {
			continue
		}
		switch index {
		case 0, 1:
			blood++
		case 2, 3:
			unholy++
		default:
			frost++
		}
	}
	return []SpellPowerData{
		{Cost: blood, Type: powerRuneBlood},
		{Cost: frost, Type: powerRuneFrost},
		{Cost: unholy, Type: powerRuneUnholy},
	}
}

// AdvanceRuneCooldowns fills the six progress bytes expected by the modern
// client. The legacy cast packet only includes bytes for runes consumed by
// that cast, so progress for runes that were already cooling must be carried
// forward between SpellGo packets. AzerothCore sends 0 for a rune that has
// just been spent; legacy proxy rewrites that to 1 before the 3.4.3 client sees it.
func AdvanceRuneCooldowns(data *RuneData, started *[runeCount]time.Time, now time.Time) {
	if data == nil || started == nil || len(data.Cooldowns) != runeCount {
		return
	}
	for index := 0; index < runeCount; index++ {
		bit := byte(1 << index)
		wasReady := data.Start&bit != 0
		isReady := data.Count&bit != 0
		if isReady {
			data.Cooldowns[index] = 0xff
			started[index] = time.Time{}
			continue
		}
		if wasReady {
			progress := data.Cooldowns[index]
			if progress == 0 {
				progress = 1
				data.Cooldowns[index] = 1
			}
			started[index] = now.Add(-time.Duration(progress) * runeRechargeDuration / 0xff)
			continue
		}
		if started[index].IsZero() {
			data.Cooldowns[index] = 0
			continue
		}
		elapsed := now.Sub(started[index])
		if elapsed <= 0 {
			data.Cooldowns[index] = 0
		} else if elapsed >= runeRechargeDuration {
			// The availability mask remains authoritative until the realm marks
			// this rune ready; 0xfe avoids contradicting that mask.
			data.Cooldowns[index] = 0xfe
		} else {
			data.Cooldowns[index] = byte(elapsed * 0xff / runeRechargeDuration)
		}
	}
}
