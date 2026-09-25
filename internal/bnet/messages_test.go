package bnet

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestDisconnectMessages(t *testing.T) {
	for _, tc := range []struct {
		payload []byte
		code    uint32
		invalid bool
	}{
		{[]byte{0x08, 0x00}, 0, false},
		{[]byte{0x08, 0xac, 0x02}, 300, false},
		{nil, 0, false},
		{[]byte{0x08, 0x01, 0x10, 0x07}, 1, false},
		{[]byte{0x0a, 0x00}, 0, true},
		{[]byte{0x08, 0x80}, 0, true},
	} {
		code, err := DecodeDisconnectRequest(tc.payload)
		if (err != nil) != tc.invalid || (!tc.invalid && code != tc.code) {
			t.Fatalf("payload %x: code=%d err=%v", tc.payload, code, err)
		}
	}
	if got := EncodeDisconnectNotification(0); !bytes.Equal(got, []byte{0x08, 0x00}) {
		t.Fatalf("zero error_code must be present: %x", got)
	}
	if got := EncodeDisconnectNotification(300); !bytes.Equal(got, []byte{0x08, 0xac, 0x02}) {
		t.Fatalf("nonzero error_code: %x", got)
	}
}

func TestDecodeLogonRequest(t *testing.T) {
	payload := EncodeLogonRequest(LogonRequest{
		Program:            "WoW",
		Platform:           "Win",
		Locale:             "zhCN",
		ApplicationVersion: 54261,
	})
	request, err := DecodeLogonRequest(payload)
	if err != nil {
		t.Fatal(err)
	}
	if request.Program != "WoW" || request.Platform != "Win" || request.Locale != "zhCN" || request.ApplicationVersion != 54261 {
		t.Fatalf("unexpected logon request: %#v", request)
	}
}

func TestConnectRequestRoundTrip(t *testing.T) {
	clientID := appendVarintField(nil, 1, 77)
	want := ConnectRequest{ClientID: clientID, UseBindlessRPC: true}
	got, err := DecodeConnectRequest(EncodeConnectRequest(want))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.ClientID, want.ClientID) || !got.UseBindlessRPC {
		t.Fatalf("unexpected connect request: %#v", got)
	}
}

func TestConnectResponseEchoesClientID(t *testing.T) {
	clientID := appendVarintField(nil, 1, 77)
	payload := EncodeConnectResponse(ConnectRequest{ClientID: clientID, UseBindlessRPC: true}, 12, 34, 56)
	var echoed []byte
	err := walkFields(payload, func(number protowire.Number, typ protowire.Type, value []byte, _ uint64) error {
		if number == 2 {
			echoed = append([]byte(nil), value...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(echoed, clientID) {
		t.Fatalf("client id %x, want %x", echoed, clientID)
	}
}

func TestConnectRequestOmitsBindlessDefaultsTrue(t *testing.T) {
	got, err := DecodeConnectRequest(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.UseBindlessRPC {
		t.Fatal("omitted use_bindless_rpc must default to true")
	}

	explicitFalse, err := DecodeConnectRequest(appendVarintField(nil, 3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if explicitFalse.UseBindlessRPC {
		t.Fatal("explicit use_bindless_rpc=false was ignored")
	}
}

func TestConnectResponseAlwaysWritesBindless(t *testing.T) {
	empty, err := DecodeConnectRequest(nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := EncodeConnectResponse(empty, 1, 2, 3)
	got := bindlessField(t, payload)
	if got != 1 {
		t.Fatalf("omitted request field produced use_bindless_rpc=%d, want 1", got)
	}

	disabled := EncodeConnectResponse(ConnectRequest{UseBindlessRPC: false}, 1, 2, 3)
	if got := bindlessField(t, disabled); got != 0 {
		t.Fatalf("explicit false produced use_bindless_rpc=%d, want 0", got)
	}
}

func bindlessField(t *testing.T, payload []byte) uint64 {
	t.Helper()
	var bindless *uint64
	err := walkFields(payload, func(number protowire.Number, _ protowire.Type, _ []byte, scalar uint64) error {
		if number == 7 {
			value := scalar
			bindless = &value
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if bindless == nil {
		t.Fatal("ConnectResponse omitted use_bindless_rpc; client default is false")
	}
	return *bindless
}

func TestWebCredentials(t *testing.T) {
	payload := EncodeWebCredentials("RS-ticket")
	ticket, err := DecodeWebCredentials(payload)
	if err != nil || ticket != "RS-ticket" {
		t.Fatalf("ticket=%q err=%v", ticket, err)
	}
}

func TestChallengeAndLogonResultRoundTrip(t *testing.T) {
	challenge, err := DecodeExternalChallenge(EncodeExternalChallenge("https://127.0.0.1:7001/login"))
	if err != nil {
		t.Fatal(err)
	}
	if challenge.PayloadType != "web_auth_url" || challenge.URL != "https://127.0.0.1:7001/login" {
		t.Fatalf("unexpected challenge: %#v", challenge)
	}

	key := bytes.Repeat([]byte{0x5a}, 64)
	result, err := DecodeLogonResult(EncodeLogonResult(key))
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode != 0 || !bytes.Equal(result.SessionKey, key) {
		t.Fatalf("unexpected logon result: %#v", result)
	}
}
