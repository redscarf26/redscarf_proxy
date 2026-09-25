package modernworld

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestAuthSessionRoundTrip(t *testing.T) {
	want := AuthSession{
		DOSResponse:     123,
		RegionID:        1,
		BattlegroupID:   1,
		RealmID:         7,
		UseIPv6:         true,
		RealmJoinTicket: "TESTER",
	}
	for index := range want.LocalChallenge {
		want.LocalChallenge[index] = byte(index)
	}
	for index := range want.Digest {
		want.Digest[index] = byte(index + 32)
	}
	got, err := ParseAuthSession(EncodeAuthSession(want))
	if err != nil {
		t.Fatal(err)
	}
	if got.DOSResponse != want.DOSResponse || got.RegionID != 1 || got.BattlegroupID != 1 || got.RealmID != 7 || !got.UseIPv6 || got.RealmJoinTicket != "TESTER" || got.LocalChallenge != want.LocalChallenge || got.Digest != want.Digest {
		t.Fatalf("unexpected auth session: %#v", got)
	}
}

func TestWorldAuthEncryptionTransition(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	_ = serverSide.SetDeadline(time.Now().Add(3 * time.Second))
	_ = clientSide.SetDeadline(time.Now().Add(3 * time.Second))
	server := NewServerConn(serverSide)
	client := newPacketConn(clientSide, clientNonceSuffix, serverNonceSuffix)
	var worldKey [64]byte
	for index := range worldKey {
		worldKey[index] = byte(index + 1)
	}
	type serverResult struct {
		challenge AuthChallenge
		err       error
	}
	serverDone := make(chan serverResult, 1)
	go func() {
		challenge, err := server.Accept()
		if err != nil {
			serverDone <- serverResult{err: err}
			return
		}
		packet, err := server.ReadPacket()
		if err != nil {
			serverDone <- serverResult{err: err}
			return
		}
		if packet.Opcode != CMSGAuthSession {
			serverDone <- serverResult{err: io.ErrUnexpectedEOF}
			return
		}
		auth, err := ParseAuthSession(packet.Body)
		if err != nil {
			serverDone <- serverResult{err: err}
			return
		}
		_, encryptionKey := DeriveKeys(worldKey, challenge.ServerChallenge, auth.LocalChallenge)
		enter, err := EncodeEnterEncryptedMode(encryptionKey)
		if err != nil {
			serverDone <- serverResult{err: err}
			return
		}
		if err := server.WritePacket(SMSGEnterEncryptedMode, enter); err != nil {
			serverDone <- serverResult{err: err}
			return
		}
		ack, err := server.ReadPacket()
		if err != nil || ack.Opcode != CMSGEnterEncryptedModeAck {
			serverDone <- serverResult{err: err}
			return
		}
		if err := server.EnableEncryption(encryptionKey[:]); err != nil {
			serverDone <- serverResult{err: err}
			return
		}
		if err := server.WritePacket(SMsgPong, []byte{1, 2, 3, 4}); err != nil {
			serverDone <- serverResult{err: err}
			return
		}
		serverDone <- serverResult{challenge: challenge}
	}()

	initializer := make([]byte, len(ServerInitializer))
	if _, err := io.ReadFull(clientSide, initializer); err != nil {
		t.Fatal(err)
	}
	if string(initializer) != ServerInitializer {
		t.Fatalf("initializer %q", initializer)
	}
	if _, err := clientSide.Write([]byte(ClientInitializer)); err != nil {
		t.Fatal(err)
	}
	challengePacket, err := client.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if challengePacket.Opcode != SMSGAuthChallenge || len(challengePacket.Body) != 49 {
		t.Fatalf("unexpected auth challenge: %#v", challengePacket)
	}
	var serverChallenge [16]byte
	copy(serverChallenge[:], challengePacket.Body[32:48])
	auth := AuthSession{RegionID: 1, BattlegroupID: 1, RealmID: 7, RealmJoinTicket: "TESTER"}
	for index := range auth.LocalChallenge {
		auth.LocalChallenge[index] = byte(0xa0 + index)
	}
	if err := client.WritePacket(CMSGAuthSession, EncodeAuthSession(auth)); err != nil {
		t.Fatal(err)
	}
	_, encryptionKey := DeriveKeys(worldKey, serverChallenge, auth.LocalChallenge)
	enterPacket, err := client.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if enterPacket.Opcode != SMSGEnterEncryptedMode {
		t.Fatalf("enter-encrypted opcode %d", enterPacket.Opcode)
	}
	if err := VerifyEnterEncryptedMode(enterPacket.Body, encryptionKey); err != nil {
		t.Fatal(err)
	}
	if err := client.WritePacket(CMSGEnterEncryptedModeAck, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.EnableEncryption(encryptionKey[:]); err != nil {
		t.Fatal(err)
	}
	pong, err := client.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if pong.Opcode != SMsgPong || !bytes.Equal(pong.Body, []byte{1, 2, 3, 4}) {
		t.Fatalf("unexpected encrypted pong: %#v", pong)
	}
	if result := <-serverDone; result.err != nil {
		t.Fatal(result.err)
	}
}

func TestDeriveKeysIsDeterministic(t *testing.T) {
	var worldKey [64]byte
	var serverChallenge, localChallenge [16]byte
	for index := range worldKey {
		worldKey[index] = byte(index)
	}
	for index := range serverChallenge {
		serverChallenge[index] = byte(index + 64)
		localChallenge[index] = byte(index + 96)
	}
	sessionA, encryptionA := DeriveKeys(worldKey, serverChallenge, localChallenge)
	sessionB, encryptionB := DeriveKeys(worldKey, serverChallenge, localChallenge)
	if sessionA != sessionB || encryptionA != encryptionB || sessionA == [40]byte{} || encryptionA == [16]byte{} {
		t.Fatal("key derivation is not deterministic")
	}
}

func TestEncodeAuthResponseContainsVirtualRealm(t *testing.T) {
	body := EncodeAuthResponse(0x01010007, "Azeroth Core", time.Unix(123456, 0))
	if len(body) < 40 {
		t.Fatalf("auth response is too short: %d", len(body))
	}
	if body[4]&0x80 == 0 || body[4]&0x40 != 0 {
		t.Fatalf("unexpected success/wait bits: 0x%02x", body[4])
	}
	if got := binary.LittleEndian.Uint32(body[23:27]); got != 10 {
		t.Fatalf("available race count = %d, want 10", got)
	}
	if !bytes.Contains(body, []byte("Azeroth Core")) || !bytes.Contains(body, []byte("AzerothCore")) {
		t.Fatalf("auth response lacks virtual realm names: %x", body)
	}
}
