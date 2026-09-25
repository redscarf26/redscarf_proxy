package proxy

import "redscarf/internal/modernworld"

func (session *proxySession) sendMapDifficulty() error {
	session.worldMu.Lock()
	info := modernworld.WorldServerInfoForDifficulty(uint32(session.currentMapID), session.mapDifficulty)
	if !session.hasMapDifficulty && modernworld.IsLegacyInstanceMap(uint32(session.currentMapID)) {
		info = modernworld.WorldServerInfo{}
	}
	session.worldMu.Unlock()
	return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGWorldServerInfo, Body: modernworld.EncodeWorldServerInfo(info)})
}
