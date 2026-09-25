package modernworld

import (
	"encoding/binary"
	"fmt"
)

// EncodeSetTimeZoneInformation uses the exact two 7-bit length fields expected
// by the 3.4.3 glue screen. The strings are intentionally IANA names, matching
// what the reference proxy sends.
func EncodeSetTimeZoneInformation(serverTimeZone, gameTimeZone string) []byte {
	if len(serverTimeZone) > 127 {
		serverTimeZone = serverTimeZone[:127]
	}
	if len(gameTimeZone) > 127 {
		gameTimeZone = gameTimeZone[:127]
	}
	bits := newBitWriter(nil)
	bits.writeBits(uint32(len(serverTimeZone)), 7)
	bits.writeBits(uint32(len(gameTimeZone)), 7)
	body := bits.flush()
	body = append(body, serverTimeZone...)
	return append(body, gameTimeZone...)
}

// EncodeFeatureSystemStatusGlue emits the build-54261 layout. Optional store,
// boost, ticket and cross-region systems are disabled; the fixed-width tail is
// still required or the client loses framing before character enumeration.
func EncodeFeatureSystemStatusGlue() []byte {
	bits := newBitWriter(nil)
	for range 30 {
		bits.writeBit(false)
	}
	body := bits.flush()
	body = binary.LittleEndian.AppendUint32(body, 0)  // token poll seconds
	body = binary.LittleEndian.AppendUint32(body, 0)  // kiosk session minutes
	body = binary.LittleEndian.AppendUint64(body, 0)  // token balance
	body = binary.LittleEndian.AppendUint32(body, 10) // max characters per realm
	body = binary.LittleEndian.AppendUint32(body, 0)  // copy-source regions
	body = binary.LittleEndian.AppendUint32(body, 0)  // store delivery delay
	body = binary.LittleEndian.AppendUint32(body, 0)  // character boost type
	body = binary.LittleEndian.AppendUint32(body, 0)  // class-trial boost type
	body = binary.LittleEndian.AppendUint32(body, 0)  // minimum expansion level
	body = binary.LittleEndian.AppendUint32(body, 2)  // maximum expansion level (WotLK)
	body = binary.LittleEndian.AppendUint32(body, 0)  // active season
	body = binary.LittleEndian.AppendUint32(body, 0)  // game-rule value count
	body = binary.LittleEndian.AppendUint16(body, 0)  // max name queries
	body = binary.LittleEndian.AppendUint16(body, 0)  // name-query telemetry interval
	body = binary.LittleEndian.AppendUint32(body, 0)  // player-name query interval
	body = binary.LittleEndian.AppendUint32(body, 0)  // debug-time event count
	body = binary.LittleEndian.AppendUint32(body, 0)  // unused 10.0.7 field
	return body
}

func EncodeCacheVersion(version uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, version)
}

// Query responses contain proxy-owned schema conversions. Include a stable
// revision so cached pre-fix GameObject templates are refreshed on login.
// XOR preserves changes to the upstream version without collisions between
// distinct upstream versions under this revision.
func EncodeLegacyCacheVersion(version uint32) []byte {
	return EncodeCacheVersion(version ^ 0x52530001)
}

const (
	CMSGChangeRealmTicket         = uint16(0x3701) // 14081
	SMSGChangeRealmTicketResponse = uint16(0x280A) // 10250
)

// ChangeRealmTicket is the in-world request that rotates the 32-byte client
// secret used to build the next realm-join world key.
type ChangeRealmTicket struct {
	Token  uint32
	Secret [32]byte
}

func ParseChangeRealmTicket(body []byte) (ChangeRealmTicket, error) {
	var request ChangeRealmTicket
	if len(body) != 36 {
		return request, fmt.Errorf("change-realm-ticket has %d bytes, want 36", len(body))
	}
	request.Token = binary.LittleEndian.Uint32(body[:4])
	copy(request.Secret[:], body[4:])
	return request, nil
}

// EncodeChangeRealmTicketResponse echoes the token with Allow set and a
// one-byte zero ticket. This proxy is the bnet endpoint, so the ticket does
// not come from an auth reconnect.
func EncodeChangeRealmTicketResponse(token uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, token)
	bits := newBitWriter(body)
	bits.writeBit(true)
	body = bits.flush()
	body = binary.LittleEndian.AppendUint32(body, 1)
	return append(body, 0)
}

func EncodeBattleNetConnectionStatus(state byte, suppressNotification bool) []byte {
	bits := newBitWriter(nil)
	bits.writeBits(uint32(state&3), 2)
	bits.writeBit(suppressNotification)
	return bits.flush()
}
