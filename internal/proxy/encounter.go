package proxy

import "redscarf/internal/modernworld"

func (session *proxySession) visibleEncounterGUIDLocked(id uint64) (modernworld.GUID128, bool) {
	guid, known := session.objectGUIDs[id]
	_, visible := session.visibleObjectGUIDs[id]
	return guid, known && visible
}

func (session *proxySession) wrapEncounterPacketsLocked(packets []modernworld.Packet) []modernworld.Packet {
	out := make([]modernworld.Packet, 0, len(packets)+2)
	for _, packet := range packets {
		if packet.Opcode == modernworld.SMSGInstanceEncounterEngageUnit && !session.encounterInProgress {
			session.encounterInProgress = true
			out = append(out, modernworld.Packet{
				Opcode: modernworld.SMSGInstanceEncounterStart,
				Body:   modernworld.EncodeInstanceEncounterStart(true),
			})
		}
		out = append(out, packet)
		if packet.Opcode == modernworld.SMSGInstanceEncounterDisengageUnit && len(session.encounterFrames) == 0 && session.encounterInProgress {
			session.encounterInProgress = false
			out = append(out, modernworld.Packet{Opcode: modernworld.SMSGInstanceEncounterEnd})
		}
	}
	return out
}

func (session *proxySession) flushEncounterFrames() error {
	session.worldMu.Lock()
	packets := session.wrapEncounterPacketsLocked(session.encounterFrames.Flush(session.visibleEncounterGUIDLocked))
	session.worldMu.Unlock()
	for _, p := range packets {
		if err := session.sendInstance(p); err != nil {
			return err
		}
	}
	return nil
}
