package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Character identity and PvP settings (build 54261 client <-> 3.3.5a
// AzerothCore). WotLK Classic moved these small character-pane controls onto
// their own packets, but the 3.3.5a server still reads the original WotLK
// layout, so each is a parse-and-reencode hop. Layouts mirror HermesProxy-WOTLK
// World/Server/Packets {SetTitle,TogglePvP,SetPvP,UnlearnSkill,RemoveGlyph}.cs
// and World/Server/WorldSocket.cs.

const (
	// Client -> server identity/PvP controls (build 54261).
	CMSGSetTitle     = uint16(12926)
	CMSGTogglePvP    = uint16(12971)
	CMSGSetPvP       = uint16(12972)
	CMSGUnlearnSkill = uint16(13541)
	CMSGRemoveGlyph  = uint16(13056)
)

// SetTitle carries the newly selected title id (0 or -1 clears it).
type SetTitleRequest struct {
	TitleID int32
}

// SetPvPRequest carries the explicit PvP state chosen by the modern client.
type SetPvPRequest struct {
	Enable bool
}

// UnlearnSkillRequest is a primary profession being dropped.
type UnlearnSkillRequest struct {
	SkillLine uint32
}

// RemoveGlyphRequest selects the glyph slot to clear.
type RemoveGlyphRequest struct {
	GlyphSlot byte
}

func ParseSetTitle(body []byte) (SetTitleRequest, error) {
	var request SetTitleRequest
	if len(body) != 4 {
		return request, fmt.Errorf("set-title packet has %d bytes, want 4", len(body))
	}
	request.TitleID = int32(binary.LittleEndian.Uint32(body))
	return request, nil
}

func EncodeLegacySetTitle(request SetTitleRequest) []byte {
	return binary.LittleEndian.AppendUint32(nil, uint32(request.TitleID))
}

func ParseSetPvP(body []byte) (SetPvPRequest, error) {
	var request SetPvPRequest
	r := movementReader{data: body}
	// The 54261 client packs the single enable bit MSB-first (a "true" state is
	// byte 0x80), exactly as HermesProxy's SetPvP reads it with HasBit.
	enable, err := r.bit()
	if err != nil {
		return request, fmt.Errorf("read set-pvp bit: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return request, fmt.Errorf("set-pvp has %d trailing bytes", r.remaining())
	}
	request.Enable = enable
	return request, nil
}

// EncodeLegacySetPvP writes the explicit one-byte PvP status that 3.3.5a
// CMSG_TOGGLE_PVP accepts as its "set" form (AzerothCore toggles only on an
// empty body, applies status on a one-byte body).
func EncodeLegacySetPvP(enable bool) []byte {
	if enable {
		return []byte{1}
	}
	return []byte{0}
}

// ParseUnlearnSkill reads the uint32 skill-line id the modern client sends.
func ParseUnlearnSkill(body []byte) (UnlearnSkillRequest, error) {
	var request UnlearnSkillRequest
	if len(body) != 4 {
		return request, fmt.Errorf("unlearn-skill packet has %d bytes, want 4", len(body))
	}
	request.SkillLine = binary.LittleEndian.Uint32(body)
	return request, nil
}

func EncodeLegacyUnlearnSkill(request UnlearnSkillRequest) []byte {
	return binary.LittleEndian.AppendUint32(nil, request.SkillLine)
}

func ParseRemoveGlyph(body []byte) (RemoveGlyphRequest, error) {
	var request RemoveGlyphRequest
	if len(body) < 1 {
		return request, fmt.Errorf("remove-glyph packet has %d bytes, want at least 1", len(body))
	}
	request.GlyphSlot = body[0]
	return request, nil
}

// EncodeLegacyRemoveGlyph widens the modern one-byte glyph slot to the uint32
// the 3.3.5a server reads from CMSG_REMOVE_GLYPH.
func EncodeLegacyRemoveGlyph(request RemoveGlyphRequest) []byte {
	return binary.LittleEndian.AppendUint32(nil, uint32(request.GlyphSlot))
}
