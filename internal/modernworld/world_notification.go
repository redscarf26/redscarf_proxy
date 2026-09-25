package modernworld

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

const SMSGGossipPOI = uint16(0x2798)

// TranslateLegacyWorldNotification handles standalone notifications, independent
// of the map-transfer lifecycle. Layout matches legacy proxy's 54261 Gossip POI branch.
func TranslateLegacyWorldNotification(opcode uint16, body []byte) (Packet, error) {
	switch opcode {
	case 0x0320:
		if len(body) != 4 {
			return Packet{}, fmt.Errorf("last instance has %d bytes, want 4", len(body))
		}
		return Packet{Opcode: SMSGUpdateLastInstance, Body: EncodeUpdateLastInstance(binary.LittleEndian.Uint32(body))}, nil
	case 0x0224:
		r := movementReader{data: body}
		// Flags, X, Y, icon and importance are five 32-bit values.
		fixed, err := r.take(20)
		if err != nil {
			return Packet{}, err
		}
		name, err := r.cstring()
		if err != nil {
			return Packet{}, err
		}
		if r.remaining() != 0 {
			return Packet{}, fmt.Errorf("gossip POI has %d trailing bytes", r.remaining())
		}
		if !utf8.ValidString(name) {
			return Packet{}, fmt.Errorf("gossip POI name is not UTF-8")
		}
		// Modern length is six bits in bytes. Keep long localized names valid.
		if len(name) > 63 {
			name = name[:63]
			for !utf8.ValidString(name) {
				name = name[:len(name)-1]
			}
		}
		out := binary.LittleEndian.AppendUint32(nil, 1) // legacy has one active POI
		out = append(out, fixed[:12]...)                // flags, X, Y
		out = appendFloat32(out, 0)                     // legacy has no Z
		out = append(out, fixed[12:]...)                // icon, importance
		out = binary.LittleEndian.AppendUint32(out, 0)  // WMO group
		bits := newBitWriter(out)
		bits.writeBits(uint32(len(name)), 6)
		out = append(bits.flush(), []byte(name)...)
		return Packet{Opcode: SMSGGossipPOI, Body: out}, nil
	default:
		return Packet{}, fmt.Errorf("unsupported world notification %#x", opcode)
	}
}
