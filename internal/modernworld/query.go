package modernworld

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	CMSGQueryTime        = uint16(13525)
	CMSGQueryRealmName   = uint16(13962)
	CMSGQueryCreature    = uint16(12912)
	CMSGQueryGameObject  = uint16(12913)
	CMSGQueryNpcText     = uint16(12914)
	CMSGQueryPlayerNames = uint16(14194)
	SMSGQueryTime        = uint16(9956)
	SMSGRealmQuery       = uint16(10515)
	SMSGQueryCreature    = uint16(10516)
	SMSGQueryGameObject  = uint16(10517)
	SMSGQueryNpcText     = uint16(10518)
	SMSGQueryPlayerNames = uint16(12315)

	npcTextSlots = 8
)

func ParseQueryTime(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("query-time has %d trailing bytes", len(body))
	}
	return nil
}

func TranslateQueryTimeResponse(legacy []byte) ([]byte, error) {
	if len(legacy) != 8 {
		return nil, fmt.Errorf("legacy query-time response has %d bytes, want 8", len(legacy))
	}
	return append([]byte(nil), legacy...), nil
}

func ParseQueryRealmName(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("query-realm-name has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func EncodeRealmQueryResponse(address uint32, name string) ([]byte, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("realm name is empty")
	}
	if !utf8.ValidString(name) {
		return nil, fmt.Errorf("realm name is not valid UTF-8")
	}
	if utf8.RuneCountInString(name) > 255 {
		return nil, fmt.Errorf("realm name has %d runes, maximum is 255", utf8.RuneCountInString(name))
	}
	normalized := strings.ReplaceAll(name, " ", "")
	if len(name) > 255 || len(normalized) > 255 {
		return nil, fmt.Errorf("realm name encoding exceeds 255 bytes")
	}
	body := binary.LittleEndian.AppendUint32(nil, address)
	body = append(body, 0) // LookupState success
	bits := newBitWriter(body)
	bits.writeBit(true) // IsLocal
	bits.writeBit(false)
	bits.writeBits(uint32(len(name)), 8)
	bits.writeBits(uint32(len(normalized)), 8)
	body = bits.flush()
	body = append(body, name...)
	return append(body, normalized...), nil
}
