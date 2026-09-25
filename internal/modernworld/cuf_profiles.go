package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Build 3.4.3.54261 opcode. This must be the wire opcode (0x25BC), not the
// similarly observed internal/config value 0x0916; the latter is silently
// ignored by the client and leaves GetNumRaidProfiles() at zero.
const SMSGLoadCUFProfiles = uint16(0x25BC)
const CMSGSaveCUFProfiles = uint16(0x318E)

// Bound disk reads and untrusted client payloads before walking their records.
const MaxCUFProfilesBytes = 64 * 1024

// ValidateCUFProfiles checks the shared SAVE/LOAD wire layout. Keep the original
// bytes so all option bits, signed offset representations and names survive.
func ValidateCUFProfiles(body []byte) error {
	if len(body) < 4 || len(body) > MaxCUFProfilesBytes {
		return fmt.Errorf("invalid CUF payload size %d", len(body))
	}
	count := binary.LittleEndian.Uint32(body)
	remaining := body[4:]
	for i := uint32(0); i < count; i++ {
		// 7 name-length bits + 21 option bits occupy 4 bytes; then 15
		// bytes of dimensions, sort/text modes, anchors and offsets.
		if len(remaining) < 19 {
			return fmt.Errorf("truncated CUF profile %d", i)
		}
		size := 19 + int(remaining[0]>>1)
		if len(remaining) < size {
			return fmt.Errorf("truncated CUF profile %d name", i)
		}
		remaining = remaining[size:]
	}
	if len(remaining) != 0 {
		return fmt.Errorf("CUF payload has %d trailing bytes", len(remaining))
	}
	return nil
}

const cufBoolOptionsCount = 21

// CUFProfile is a Compact Unit Frame profile used by the built-in raid frame.
// The option indices match build 54261's CUFBoolOptions enumeration.
type CUFProfile struct {
	Name         string
	FrameHeight  uint16
	FrameWidth   uint16
	SortBy       byte
	HealthText   byte
	TopPoint     byte
	BottomPoint  byte
	LeftPoint    byte
	TopOffset    uint16
	BottomOffset uint16
	LeftOffset   uint16
	BoolOptions  [cufBoolOptionsCount]bool
}

func DefaultCUFProfiles() []CUFProfile {
	profile := CUFProfile{
		Name:        "主配置",
		FrameHeight: 36,
		FrameWidth:  72,
	}
	// These are the options stored by legacy proxy for the same accounts:
	// display main tank/assist, border, non-boss debuffs, dynamic position,
	// locked, and most importantly CUF_SHOWN.
	for _, option := range []int{2, 5, 8, 9, 10, 11} {
		profile.BoolOptions[option] = true
	}
	return []CUFProfile{profile}
}

func EncodeLoadCUFProfiles(profiles []CUFProfile) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(profiles)))
	for _, profile := range profiles {
		name := []byte(profile.Name)
		bits := newBitWriter(body)
		bits.writeBits(uint32(len(name)), 7)
		for _, enabled := range profile.BoolOptions {
			bits.writeBit(enabled)
		}
		body = bits.flush()

		body = binary.LittleEndian.AppendUint16(body, profile.FrameHeight)
		body = binary.LittleEndian.AppendUint16(body, profile.FrameWidth)
		body = append(body, profile.SortBy, profile.HealthText)
		body = append(body, profile.TopPoint, profile.BottomPoint, profile.LeftPoint)
		body = binary.LittleEndian.AppendUint16(body, profile.TopOffset)
		body = binary.LittleEndian.AppendUint16(body, profile.BottomOffset)
		body = binary.LittleEndian.AppendUint16(body, profile.LeftOffset)
		body = append(body, name...)
	}
	return body
}
