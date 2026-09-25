package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGQueryCorpseLocation = uint16(13922)
	CMSGReclaimCorpse       = uint16(13531)
	CMSGRepopRequest        = uint16(13606)
	CMSGRequestCemeteryList = uint16(12665)
	SMSGCorpseLocation      = uint16(9807)
	SMSGDeathReleaseLoc     = uint16(9939)
	SMSGCorpseReclaimDelay  = uint16(10058)
)

type CorpseLocation struct {
	Valid       bool
	ActualMapID int32
	X           float32
	Y           float32
	Z           float32
	MapID       int32
}

func ParseQueryCorpseLocation(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ParseRepopRequest(body []byte) (bool, error) {
	if len(body) == 0 {
		return false, nil
	}
	r := movementReader{data: body}
	checkInstance, err := r.bit()
	if err != nil {
		return false, fmt.Errorf("read repop CheckInstance: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return false, fmt.Errorf("repop-request has %d trailing bytes", r.remaining())
	}
	return checkInstance, nil
}

func EncodeLegacyRepopRequest(checkInstance bool) []byte {
	if checkInstance {
		return []byte{1}
	}
	return []byte{0}
}

func ParseReclaimCorpse(body []byte) (GUID128, error) {
	if len(body) == 0 {
		return GUID128{}, nil
	}
	return ParsePackedGUID128Exact(body)
}

func EncodeCorpseReclaimDelay(delayMS uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, delayMS)
}

func ParseLegacyCorpseQuery(body []byte) (CorpseLocation, error) {
	var loc CorpseLocation
	r := movementReader{data: body}
	found, err := r.u8()
	if err != nil {
		return loc, fmt.Errorf("read corpse-query found flag: %w", err)
	}
	if found == 0 {
		if r.remaining() != 0 {
			return loc, fmt.Errorf("empty corpse-query has %d trailing bytes", r.remaining())
		}
		return loc, nil
	}
	mapID, err := r.i32()
	if err != nil {
		return loc, fmt.Errorf("read corpse-query map: %w", err)
	}
	x, err := r.f32()
	if err != nil {
		return loc, fmt.Errorf("read corpse-query x: %w", err)
	}
	y, err := r.f32()
	if err != nil {
		return loc, fmt.Errorf("read corpse-query y: %w", err)
	}
	z, err := r.f32()
	if err != nil {
		return loc, fmt.Errorf("read corpse-query z: %w", err)
	}
	actualMap, err := r.i32()
	if err != nil {
		return loc, fmt.Errorf("read corpse-query actual map: %w", err)
	}
	if r.remaining() >= 4 {
		if _, err := r.u32(); err != nil {
			return loc, fmt.Errorf("read corpse-query extra: %w", err)
		}
	}
	if r.remaining() != 0 {
		return loc, fmt.Errorf("corpse-query has %d trailing bytes", r.remaining())
	}
	loc.Valid = true
	loc.MapID = mapID
	loc.X = x
	loc.Y = y
	loc.Z = z
	loc.ActualMapID = actualMap
	return loc, nil
}

func EncodeCorpseLocation(player GUID128, loc CorpseLocation) []byte {
	bits := newBitWriter(nil)
	bits.writeBit(loc.Valid)
	body := bits.flush()
	body = appendPackedGUID128(body, player.Low, player.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(loc.ActualMapID))
	body = appendFloat32(body, loc.X)
	body = appendFloat32(body, loc.Y)
	body = appendFloat32(body, loc.Z)
	body = binary.LittleEndian.AppendUint32(body, uint32(loc.MapID))
	return appendPackedGUID128(body, 0, 0)
}

func TranslateDeathReleaseLoc(legacy []byte) ([]byte, error) {
	if len(legacy) != 16 {
		return nil, fmt.Errorf("legacy death-release-loc has %d bytes, want 16", len(legacy))
	}
	return append([]byte(nil), legacy...), nil
}

func TranslateCorpseReclaimDelay(legacy []byte) ([]byte, error) {
	if len(legacy) != 4 {
		return nil, fmt.Errorf("legacy corpse-reclaim-delay has %d bytes, want 4", len(legacy))
	}
	return append([]byte(nil), legacy...), nil
}
