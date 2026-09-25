package proxy

import "redscarf/internal/modernworld"

func (s *Server) handleModernVehicle(session *proxySession, packet modernworld.Packet) (bool, error) {
	session.worldMu.Lock()
	opcode, body, handled, err := modernworld.TranslateVehicleRequest(packet.Opcode, packet.Body, session.legacyGUIDForModernLocked)
	conn := session.legacyWorld
	session.worldMu.Unlock()
	if !handled || err != nil || conn == nil {
		return handled, err
	}
	if packet.Opcode == modernworld.CMSGRideVehicleInteract {
		s.log.Debug("ride vehicle interact", "account", session.legacy.Username, "legacy_opcode", opcode, "bytes", len(body))
	}
	return true, conn.WritePacket(opcode, body)
}
