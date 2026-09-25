package modernworld

import (
	"encoding/binary"
	"math"
	"net"
	"testing"
)

func TestContinuedSessionRoundTripAndKeys(t *testing.T) {
	var key [40]byte
	var serverChallenge [16]byte
	want := AuthContinuedSession{DOSResponse: 7, Key: 0x123456789abcdef0}
	for index := range key {
		key[index] = byte(index + 1)
	}
	for index := range serverChallenge {
		serverChallenge[index] = byte(0x40 + index)
		want.LocalChallenge[index] = byte(0x80 + index)
	}
	want.Digest, _ = DeriveContinuedKeys(key, want.Key, serverChallenge, want.LocalChallenge)
	got, err := ParseAuthContinuedSession(EncodeAuthContinuedSession(want))
	if err != nil {
		t.Fatal(err)
	}
	if got != want || !VerifyContinuedDigest(key, serverChallenge, got) {
		t.Fatalf("continued session mismatch: got=%#v want=%#v", got, want)
	}
	got.Digest[0] ^= 1
	if VerifyContinuedDigest(key, serverChallenge, got) {
		t.Fatal("modified continued-session digest was accepted")
	}
}

func TestConnectToEncodingAndSignature(t *testing.T) {
	body, err := EncodeConnectTo("127.0.0.1", 7002, ConnectToWorldAttempt1, 0xfedcba9876543210)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyConnectTo(body); err != nil {
		t.Fatal(err)
	}
	packet, err := ParseConnectTo(body)
	if err != nil {
		t.Fatal(err)
	}
	if packet.AddressType != 1 || !packet.IP.Equal(net.IPv4(127, 0, 0, 1)) || packet.Port != 7002 || packet.Serial != 17 || packet.Connection != 1 || packet.Key != 0xfedcba9876543210 {
		t.Fatalf("unexpected connect-to packet: %#v", packet)
	}
	body[260] ^= 1
	if VerifyConnectTo(body) == nil {
		t.Fatal("modified connect-to address was accepted")
	}
}

func TestPlayerLoginAndLoginVerifyWorld(t *testing.T) {
	body := EncodePlayerLogin(0x42, 777.5)
	login, err := ParsePlayerLogin(body)
	if err != nil {
		t.Fatal(err)
	}
	if login.GUID != 0x42 || login.FarClip != 777.5 {
		t.Fatalf("unexpected player login: %#v", login)
	}
	legacy := binary.LittleEndian.AppendUint32(nil, 571)
	for _, value := range []float32{1.25, 2.5, 3.75, 4.5} {
		legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(value))
	}
	modern, err := EncodeLoginVerifyWorld(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(modern) != 24 || binary.LittleEndian.Uint32(modern[:4]) != 571 || binary.LittleEndian.Uint32(modern[20:]) != 0 {
		t.Fatalf("unexpected login-verify-world: %x", modern)
	}
}
