package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGLogoutRequest = uint16(0x34D6)
	CMSGLogoutCancel  = uint16(0x34D8)

	// Build 3.4.3.54261 wire opcodes from legacy proxy's expansion-80 OpcodeStruct.
	// Do not use the sequential 0x091A-0x091C public opcode identifiers:
	// legacy proxy converts those through PublicOpcodeToModern before transmission.
	SMSGLogoutResponse  = uint16(0x2683)
	SMSGLogoutComplete  = uint16(0x2684)
	SMSGLogoutCancelAck = uint16(0x2685)
)

type LogoutResponse struct {
	Result  int32
	Instant bool
}

func ParseLogoutRequest(body []byte) (idle bool, err error) {
	if len(body) != 1 {
		return false, fmt.Errorf("logout-request has %d bytes, want 1", len(body))
	}
	return body[0]&1 != 0, nil
}

func ParseLogoutCancel(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("logout-cancel has %d bytes, want 0", len(body))
	}
	return nil
}

func ParseLegacyLogoutResponse(body []byte) (LogoutResponse, error) {
	if len(body) != 5 {
		return LogoutResponse{}, fmt.Errorf("legacy logout-response has %d bytes, want 5", len(body))
	}
	return LogoutResponse{
		Result:  int32(binary.LittleEndian.Uint32(body[:4])),
		Instant: body[4] != 0,
	}, nil
}

func EncodeLogoutResponse(response LogoutResponse) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(response.Result))
	if response.Instant {
		return append(body, 1)
	}
	return append(body, 0)
}

// legacy proxy's build-54261 LogoutComplete writer emits no payload.
func EncodeLogoutComplete() []byte { return nil }

func EncodeLogoutCancelAck() []byte { return nil }
