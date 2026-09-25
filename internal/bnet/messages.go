package bnet

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
)

type ConnectRequest struct {
	ClientID       []byte
	UseBindlessRPC bool
}

// DecodeDisconnectRequest reads ConnectionService.RequestDisconnect.error_code.
func DecodeDisconnectRequest(data []byte) (uint32, error) {
	var code uint32
	err := walkFields(data, func(number protowire.Number, typ protowire.Type, value []byte, scalar uint64) error {
		if number == 1 {
			if typ != protowire.VarintType {
				return fmt.Errorf("disconnect error_code wire type %d", typ)
			}
			code = uint32(scalar)
		}
		return nil
	})
	return code, err
}

func EncodeDisconnectNotification(code uint32) []byte {
	return appendVarintField(nil, 1, uint64(code))
}

type LogonRequest struct {
	Program            string
	Platform           string
	Locale             string
	ApplicationVersion uint32
}

type ExternalChallenge struct {
	PayloadType string
	URL         string
}

type LogonResult struct {
	ErrorCode  uint32
	SessionKey []byte
}

func EncodeConnectRequest(request ConnectRequest) []byte {
	var data []byte
	if len(request.ClientID) > 0 {
		data = appendBytesField(data, 1, request.ClientID)
	}
	if request.UseBindlessRPC {
		data = appendVarintField(data, 3, 1)
	}
	return data
}

func DecodeConnectRequest(data []byte) (ConnectRequest, error) {
	// proto2 default: optional bool use_bindless_rpc = 3 [default = true]
	request := ConnectRequest{UseBindlessRPC: true}
	err := walkFields(data, func(number protowire.Number, typ protowire.Type, value []byte, scalar uint64) error {
		switch number {
		case 1:
			if typ != protowire.BytesType {
				return fmt.Errorf("connect client_id wire type %d", typ)
			}
			request.ClientID = append([]byte(nil), value...)
		case 3:
			if typ != protowire.VarintType {
				return fmt.Errorf("connect use_bindless_rpc wire type %d", typ)
			}
			request.UseBindlessRPC = scalar != 0
		}
		return nil
	})
	return request, err
}

func EncodeConnectResponse(request ConnectRequest, processID, epoch uint32, serverTime uint64) []byte {
	serverID := appendVarintField(nil, 1, uint64(processID))
	serverID = appendVarintField(serverID, 2, uint64(epoch))
	data := appendBytesField(nil, 1, serverID)
	if len(request.ClientID) > 0 {
		data = appendBytesField(data, 2, request.ClientID)
	}
	data = appendVarintField(data, 6, serverTime)
	// ConnectResponse.use_bindless_rpc defaults to false. The 3.4.3 client omits
	// the request field (default true); if we also omit the response field it
	// waits forever on Bind after "BattleNet Front Connected".
	bindless := uint64(0)
	if request.UseBindlessRPC {
		bindless = 1
	}
	data = appendVarintField(data, 7, bindless)
	return data
}

func EncodeLogonRequest(request LogonRequest) []byte {
	data := appendBytesField(nil, 1, []byte(request.Program))
	data = appendBytesField(data, 2, []byte(request.Platform))
	data = appendBytesField(data, 3, []byte(request.Locale))
	return appendVarintField(data, 6, uint64(request.ApplicationVersion))
}

func DecodeLogonRequest(data []byte) (LogonRequest, error) {
	var request LogonRequest
	err := walkFields(data, func(number protowire.Number, typ protowire.Type, value []byte, scalar uint64) error {
		switch number {
		case 1, 2, 3:
			if typ != protowire.BytesType {
				return fmt.Errorf("logon string field %d wire type %d", number, typ)
			}
			switch number {
			case 1:
				request.Program = string(value)
			case 2:
				request.Platform = string(value)
			case 3:
				request.Locale = string(value)
			}
		case 6:
			if typ != protowire.VarintType {
				return fmt.Errorf("application_version wire type %d", typ)
			}
			request.ApplicationVersion = uint32(scalar)
		}
		return nil
	})
	return request, err
}

func DecodeWebCredentials(data []byte) (string, error) {
	var credentials string
	err := walkFields(data, func(number protowire.Number, typ protowire.Type, value []byte, _ uint64) error {
		if number == 1 {
			if typ != protowire.BytesType {
				return fmt.Errorf("web_credentials wire type %d", typ)
			}
			credentials = string(value)
		}
		return nil
	})
	if err == nil && credentials == "" {
		err = fmt.Errorf("web credentials are empty")
	}
	return credentials, err
}

func EncodeWebCredentials(ticket string) []byte {
	return appendBytesField(nil, 1, []byte(ticket))
}

func EncodeExternalChallenge(url string) []byte {
	data := appendBytesField(nil, 2, []byte("web_auth_url"))
	return appendBytesField(data, 3, []byte(url))
}

func DecodeExternalChallenge(data []byte) (ExternalChallenge, error) {
	var challenge ExternalChallenge
	err := walkFields(data, func(number protowire.Number, typ protowire.Type, value []byte, _ uint64) error {
		if number != 2 && number != 3 {
			return nil
		}
		if typ != protowire.BytesType {
			return fmt.Errorf("external challenge field %d wire type %d", number, typ)
		}
		if number == 2 {
			challenge.PayloadType = string(value)
		} else {
			challenge.URL = string(value)
		}
		return nil
	})
	return challenge, err
}

func EncodeLogonResult(sessionKey []byte, gameAccountIDs ...uint64) []byte {
	gameAccountLow := uint64(1)
	if len(gameAccountIDs) != 0 && gameAccountIDs[0] != 0 {
		gameAccountLow = gameAccountIDs[0]
	}
	accountID := encodeEntityID(0x0100000000000000, 1)
	gameAccountID := encodeEntityID(0x0200000200576f51, gameAccountLow)
	data := appendVarintField(nil, 1, 0)
	data = appendBytesField(data, 2, accountID)
	data = appendBytesField(data, 3, gameAccountID)
	return appendBytesField(data, 9, sessionKey)
}

func DecodeLogonResult(data []byte) (LogonResult, error) {
	var result LogonResult
	err := walkFields(data, func(number protowire.Number, typ protowire.Type, value []byte, scalar uint64) error {
		switch number {
		case 1:
			if typ != protowire.VarintType {
				return fmt.Errorf("logon result error_code wire type %d", typ)
			}
			result.ErrorCode = uint32(scalar)
		case 9:
			if typ != protowire.BytesType {
				return fmt.Errorf("logon result session_key wire type %d", typ)
			}
			result.SessionKey = append([]byte(nil), value...)
		}
		return nil
	})
	return result, err
}

func encodeEntityID(high, low uint64) []byte {
	data := protowire.AppendTag(nil, 1, protowire.Fixed64Type)
	data = protowire.AppendFixed64(data, high)
	data = protowire.AppendTag(data, 2, protowire.Fixed64Type)
	return protowire.AppendFixed64(data, low)
}

type fieldVisitor func(number protowire.Number, typ protowire.Type, bytesValue []byte, scalar uint64) error

func walkFields(data []byte, visitor fieldVisitor) error {
	for len(data) > 0 {
		number, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
		switch typ {
		case protowire.VarintType:
			value, consumed := protowire.ConsumeVarint(data)
			if consumed < 0 {
				return protowire.ParseError(consumed)
			}
			if err := visitor(number, typ, nil, value); err != nil {
				return err
			}
			data = data[consumed:]
		case protowire.Fixed32Type:
			value, consumed := protowire.ConsumeFixed32(data)
			if consumed < 0 {
				return protowire.ParseError(consumed)
			}
			if err := visitor(number, typ, nil, uint64(value)); err != nil {
				return err
			}
			data = data[consumed:]
		case protowire.Fixed64Type:
			value, consumed := protowire.ConsumeFixed64(data)
			if consumed < 0 {
				return protowire.ParseError(consumed)
			}
			if err := visitor(number, typ, nil, value); err != nil {
				return err
			}
			data = data[consumed:]
		case protowire.BytesType:
			value, consumed := protowire.ConsumeBytes(data)
			if consumed < 0 {
				return protowire.ParseError(consumed)
			}
			if err := visitor(number, typ, value, 0); err != nil {
				return err
			}
			data = data[consumed:]
		default:
			consumed := protowire.ConsumeFieldValue(number, typ, data)
			if consumed < 0 {
				return protowire.ParseError(consumed)
			}
			data = data[consumed:]
		}
	}
	return nil
}

func appendVarintField(data []byte, number protowire.Number, value uint64) []byte {
	data = protowire.AppendTag(data, number, protowire.VarintType)
	return protowire.AppendVarint(data, value)
}

func appendBytesField(data []byte, number protowire.Number, value []byte) []byte {
	data = protowire.AppendTag(data, number, protowire.BytesType)
	return protowire.AppendBytes(data, value)
}
