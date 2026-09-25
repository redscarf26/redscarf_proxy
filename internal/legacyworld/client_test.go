package legacyworld

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"redscarf/internal/legacyauth"
)

func TestAuthenticateLegacyWorld(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	var port uint16
	if _, err := fmt.Sscan(portText, &port); err != nil {
		t.Fatal(err)
	}
	session := &legacyauth.Session{Username: "TESTER"}
	for index := range session.SessionKey {
		session.SessionKey[index] = byte(index + 1)
	}
	realm := legacyauth.Realm{ID: 7, Address: host, Port: port}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serveLegacyWorldHandshake(listener, session)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, err := Authenticate(ctx, realm, session)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func serveLegacyWorldHandshake(listener net.Listener, session *legacyauth.Session) error {
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	const serverSeed = uint32(0x1234abcd)
	challenge := make([]byte, 40)
	binary.LittleEndian.PutUint32(challenge[0:4], 1)
	binary.LittleEndian.PutUint32(challenge[4:8], serverSeed)
	if err := writeServerPacket(conn, opAuthChallenge, challenge, nil); err != nil {
		return err
	}
	header := make([]byte, 6)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	size := int(binary.BigEndian.Uint16(header[:2]))
	if binary.LittleEndian.Uint32(header[2:]) != opAuthSession || size < 4 {
		return fmt.Errorf("unexpected auth-session header %x", header)
	}
	body := make([]byte, size-4)
	if _, err := io.ReadFull(conn, body); err != nil {
		return err
	}
	if binary.LittleEndian.Uint32(body[:4]) != Build335a || binary.LittleEndian.Uint32(body[4:8]) != 7 {
		return fmt.Errorf("unexpected auth-session build/realm")
	}
	terminator := bytes.IndexByte(body[8:], 0)
	if terminator < 0 || string(body[8:8+terminator]) != "TESTER" {
		return fmt.Errorf("unexpected auth-session username")
	}
	position := 8 + terminator + 1
	if len(body) < position+4+4+12+8+20 {
		return fmt.Errorf("auth-session body is short")
	}
	position += 4 // login server type
	clientSeed := binary.LittleEndian.Uint32(body[position : position+4])
	position += 4
	position += 12 // region, battlegroup, realm
	position += 8  // DOS response
	gotDigest := body[position : position+20]
	var digestInput bytes.Buffer
	digestInput.WriteString("TESTER")
	_ = binary.Write(&digestInput, binary.LittleEndian, uint32(0))
	_ = binary.Write(&digestInput, binary.LittleEndian, clientSeed)
	_ = binary.Write(&digestInput, binary.LittleEndian, serverSeed)
	digestInput.Write(session.SessionKey[:])
	wantDigest := sha1.Sum(digestInput.Bytes())
	if !bytes.Equal(gotDigest, wantDigest[:]) {
		return fmt.Errorf("auth-session digest mismatch")
	}

	send, err := newDroppedRC4(serverToClientSeed, session.SessionKey[:])
	if err != nil {
		return err
	}
	return writeServerPacket(conn, opAuthResponse, []byte{authOK}, send)
}

func writeServerPacket(w io.Writer, opcode uint16, body []byte, cipher *rc4.Cipher) error {
	header := make([]byte, 4)
	binary.BigEndian.PutUint16(header[:2], uint16(len(body)+2))
	binary.LittleEndian.PutUint16(header[2:], opcode)
	if cipher != nil {
		cipher.XORKeyStream(header, header)
	}
	if err := writeAll(w, header); err != nil {
		return err
	}
	return writeAll(w, body)
}

func TestEmptyAddonInfoIncludesTimestamp(t *testing.T) {
	blob, err := emptyAddonInfo()
	if err != nil {
		t.Fatal(err)
	}
	if len(blob) < 5 {
		t.Fatalf("addon blob too short: %d", len(blob))
	}
	wantSize := binary.LittleEndian.Uint32(blob[:4])
	if wantSize != 8 {
		t.Fatalf("uncompressed addon size %d, want 8 (count + currentTime)", wantSize)
	}
	zr, err := zlib.NewReader(bytes.NewReader(blob[4:]))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(zr)
	_ = zr.Close()
	if err != nil {
		t.Fatal(err)
	}
	if uint32(len(plain)) != wantSize {
		t.Fatalf("inflated %d bytes, want %d", len(plain), wantSize)
	}
	if binary.LittleEndian.Uint32(plain[:4]) != 0 || binary.LittleEndian.Uint32(plain[4:8]) != 0 {
		t.Fatalf("empty addon payload %x, want 8 zero bytes", plain)
	}
}
