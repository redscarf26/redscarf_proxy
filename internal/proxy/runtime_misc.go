package proxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
)

var errInvalidRuntimeMiscPacket = errors.New("invalid runtime misc packet")

func (s *Server) handleLegacyRuntimeMisc(session *proxySession, p legacyworld.Packet) error {
	if p.Opcode == 0x369 {
		if len(p.Body) != 1 || p.Body[0] > 1 {
			return fmt.Errorf("%w: invalid legacy LFG search flag", errInvalidRuntimeMiscPacket)
		}
		// Legacy search notification has no 54261 counterpart. It is distinct
		// from the implemented dungeon queue/proposal/status messages.
		s.log.Debug("legacy LFG search notification consumed", "searching", p.Body[0] != 0)
		return nil
	}
	n := 8
	if p.Opcode == 0x278 {
		n = 12
	}
	if len(p.Body) != n {
		return fmt.Errorf("%w: object notification length %d, expected %d", errInvalidRuntimeMiscPacket, len(p.Body), n)
	}
	guid := binary.LittleEndian.Uint64(p.Body[n-8:])
	session.worldMu.Lock()
	g := session.modernGUIDForLegacyLocked(guid)
	position := session.objectPositions[guid]
	session.worldMu.Unlock()
	if p.Opcode == 0x1DF {
		return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPageText, Body: modernworld.EncodePageTextObject(g)})
	}
	body := modernworld.EncodeObjectSound(binary.LittleEndian.Uint32(p.Body), g, modernworld.GUID128{}, position)
	return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPlayObjectSound, Body: body})
}
