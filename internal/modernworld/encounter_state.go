package modernworld

import (
	"encoding/binary"
	"fmt"
	"sort"
)

type EncounterEvent struct {
	Kind           uint32
	GUID           uint64
	Param1, Param2 byte
}

func ParseEncounterEvent(body []byte) (EncounterEvent, error) {
	r := legacyUpdateReader{data: body}
	var e EncounterEvent
	var err error
	if e.Kind, err = r.u32(); err != nil {
		return e, err
	}
	switch e.Kind {
	case 0, 1, 2:
		if e.GUID, err = r.packedGUID(); err != nil {
			return e, err
		}
		if e.Param1, err = r.u8(); err != nil {
			return e, err
		}
	case 3, 4, 6:
		if e.Param1, err = r.u8(); err != nil {
			return e, err
		}
	case 5:
		if e.Param1, err = r.u8(); err != nil {
			return e, err
		}
		if e.Param2, err = r.u8(); err != nil {
			return e, err
		}
	case 7:
	default:
		return e, fmt.Errorf("unknown encounter event %d", e.Kind)
	}
	if r.remaining() != 0 {
		return e, fmt.Errorf("encounter event %d has trailing bytes", e.Kind)
	}
	return e, nil
}

type EncounterFrame struct {
	Priority byte
	SentGUID GUID128
	Pending  bool
}
type EncounterFrames map[uint64]EncounterFrame

// Apply retains engagement across visibility loss, not across map changes.
// Refresh never invents a GUID or recreates an object that is not visible.
func (frames EncounterFrames) Apply(e EncounterEvent, resolve func(uint64) (GUID128, bool)) []Packet {
	if e.Kind == 7 {
		for guid, frame := range frames {
			frame.Pending = true
			frames[guid] = frame
		}
		return frames.Flush(resolve)
	}
	if e.Kind > 2 {
		return nil
	}
	if e.Kind == 1 {
		previous := frames[e.GUID]
		delete(frames, e.GUID)
		if guid, ok := resolve(e.GUID); ok {
			return []Packet{encounterFramePacket(1, guid, 0)}
		}
		if previous.SentGUID != (GUID128{}) {
			return []Packet{encounterFramePacket(1, previous.SentGUID, 0)}
		}
		return nil
	}
	frame, exists := frames[e.GUID]
	if e.Kind == 2 && !exists {
		return nil
	}
	kind := e.Kind
	if kind == 2 && frame.Pending {
		kind = 0 // Visibility was lost or the initial engagement has not been sent.
	}
	frame.Priority = e.Param1
	frame.Pending = true
	frames[e.GUID] = frame
	if guid, ok := resolve(e.GUID); ok {
		frame.Pending = false
		frame.SentGUID = guid
		frames[e.GUID] = frame
		return []Packet{encounterFramePacket(kind, guid, e.Param1)}
	}
	return nil
}

func (frames EncounterFrames) Hide(guid uint64) {
	if frame, ok := frames[guid]; ok {
		frame.Pending = true
		frames[guid] = frame
	}
}

func (frames EncounterFrames) Flush(resolve func(uint64) (GUID128, bool)) []Packet {
	ids := make([]uint64, 0, len(frames))
	for guid, frame := range frames {
		if frame.Pending {
			ids = append(ids, guid)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var out []Packet
	for _, id := range ids {
		if guid, ok := resolve(id); ok {
			frame := frames[id]
			out = append(out, encounterFramePacket(0, guid, frame.Priority))
			frame.Pending = false
			frame.SentGUID = guid
			frames[id] = frame
		}
	}
	return out
}

func encounterFramePacket(kind uint32, guid GUID128, priority byte) Packet {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	if kind != 1 {
		body = append(body, priority)
	}
	return Packet{Opcode: SMSGInstanceEncounterEngageUnit + uint16(kind), Body: body}
}

func encodeEncounterCounter(e EncounterEvent) Packet {
	opcodes := map[uint32]uint16{3: 0x27B3, 4: 0x27B4, 5: 0x27B9, 6: 0x27B5}
	body := binary.LittleEndian.AppendUint32(nil, uint32(e.Param1))
	if e.Kind == 5 {
		body = binary.LittleEndian.AppendUint32(body, uint32(e.Param2))
	}
	return Packet{Opcode: opcodes[e.Kind], Body: body}
}
