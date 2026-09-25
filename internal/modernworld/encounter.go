package modernworld

import "fmt"

const (
	SMSGInstanceEncounterEngageUnit     = uint16(0x27B0)
	SMSGInstanceEncounterDisengageUnit  = uint16(0x27B1)
	SMSGInstanceEncounterChangePriority = uint16(0x27B2)
	SMSGInstanceEncounterStart          = uint16(0x27B6)
	SMSGInstanceEncounterEnd            = uint16(0x27BA)
)

// EncodeInstanceEncounterStart writes the 54261 packet. WotLK has no combat-res
// charges; InProgress stays true while any engage frame is live. 3.4.3 keeps the
// raid combat lock until SMSG_INSTANCE_ENCOUNTER_END.
func EncodeInstanceEncounterStart(inProgress bool) []byte {
	body := make([]byte, 16)
	bits := newBitWriter(body)
	bits.writeBit(inProgress)
	return bits.flush()
}

// WotLK multiplexes events; 54261 splits them into distinct opcodes. Refresh
// (7) is stateful and must be handled by EncounterFrames, never by a GUID parser.
func TranslateEncounterUnit(body []byte, resolve func(uint64) GUID128) (Packet, error) {
	e, err := ParseEncounterEvent(body)
	if err != nil {
		return Packet{}, err
	}
	if e.Kind >= 3 && e.Kind <= 6 {
		return encodeEncounterCounter(e), nil
	}
	if e.Kind == 7 {
		return Packet{}, fmt.Errorf("encounter refresh requires session state")
	}
	if resolve == nil {
		return Packet{}, fmt.Errorf("encounter frame requires GUID resolver")
	}
	guid := resolve(e.GUID)
	if guid == (GUID128{}) {
		return Packet{}, fmt.Errorf("unknown encounter GUID")
	}
	return encounterFramePacket(e.Kind, guid, e.Param1), nil
}
