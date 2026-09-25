package legacyauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	opLogonChallenge = 0x00
	opLogonProof     = 0x01
	opRealmList      = 0x10

	ResultSuccess           ResultCode = 0x00
	ResultBanned            ResultCode = 0x03
	ResultUnknownAccount    ResultCode = 0x04
	ResultIncorrectPassword ResultCode = 0x05
	ResultVersionInvalid    ResultCode = 0x09
	ResultSuspended         ResultCode = 0x0c
)

type ResultCode byte

type AuthError struct {
	Code ResultCode
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("legacy authentication failed (0x%02x)", byte(e.Code))
}

type Credentials struct {
	Username string
	Password string
	Locale   string
}

type Realm struct {
	ID             uint32
	Type           byte
	Locked         bool
	Flags          byte
	Name           string
	Address        string
	Port           uint16
	Population     float32
	CharacterCount byte
	Timezone       byte
	VersionMajor   byte
	VersionMinor   byte
	VersionBugfix  byte
	Build          uint16
}

type Session struct {
	Username   string
	SessionKey [40]byte
	Realms     []Realm
}

func Login(ctx context.Context, address string, credentials Credentials) (*Session, error) {
	username := strings.ToUpper(strings.TrimSpace(credentials.Username))
	// AzerothCore 3.3.5a caps the challenge payload at the fixed header plus
	// 16 account-name bytes and closes the socket when that bound is exceeded.
	if username == "" || len(username) > 16 {
		return nil, fmt.Errorf("invalid account name length")
	}
	locale := credentials.Locale
	if len(locale) != 4 {
		locale = "enUS"
	}

	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connect legacy auth %s: %w", address, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	}

	if _, err := conn.Write(logonChallengePacket(username, locale)); err != nil {
		return nil, fmt.Errorf("send logon challenge: %w", err)
	}
	challenge, err := readLogonChallenge(conn)
	if err != nil {
		return nil, err
	}
	proof, err := computeProof(challenge, username, credentials.Password, rand.Reader)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(logonProofPacket(proof)); err != nil {
		return nil, fmt.Errorf("send logon proof: %w", err)
	}
	if err := readLogonProof(conn, proof.ExpectedM2); err != nil {
		return nil, err
	}
	if _, err := conn.Write([]byte{opRealmList, 0, 0, 0, 0}); err != nil {
		return nil, fmt.Errorf("request realm list: %w", err)
	}
	realms, err := readRealmList(conn)
	if err != nil {
		return nil, err
	}
	return &Session{Username: username, SessionKey: proof.SessionKey, Realms: realms}, nil
}

func logonChallengePacket(username, locale string) []byte {
	var packet bytes.Buffer
	packet.WriteByte(opLogonChallenge)
	packet.WriteByte(8)
	_ = binary.Write(&packet, binary.LittleEndian, uint16(len(username)+30))
	packet.WriteString("WoW")
	packet.WriteByte(0)
	packet.Write([]byte{3, 3, 5})
	_ = binary.Write(&packet, binary.LittleEndian, uint16(12340))
	packet.Write(reverseASCII("x86"))
	packet.WriteByte(0)
	packet.Write(reverseASCII("Win"))
	packet.WriteByte(0)
	packet.Write(reverseASCII(locale))
	_ = binary.Write(&packet, binary.LittleEndian, uint32(60))
	_ = binary.Write(&packet, binary.LittleEndian, uint32(0x0100007f))
	packet.WriteByte(byte(len(username)))
	packet.WriteString(username)
	return packet.Bytes()
}

func readLogonChallenge(r io.Reader) (srpChallenge, error) {
	opcode, err := readByte(r)
	if err != nil {
		return srpChallenge{}, fmt.Errorf("read logon challenge opcode: %w", err)
	}
	if opcode != opLogonChallenge {
		return srpChallenge{}, fmt.Errorf("unexpected auth opcode 0x%02x", opcode)
	}
	if _, err := readByte(r); err != nil { // protocol status, always zero
		return srpChallenge{}, err
	}
	result, err := readByte(r)
	if err != nil {
		return srpChallenge{}, err
	}
	if ResultCode(result) != ResultSuccess {
		return srpChallenge{}, &AuthError{Code: ResultCode(result)}
	}
	b := make([]byte, 32)
	if _, err := io.ReadFull(r, b); err != nil {
		return srpChallenge{}, err
	}
	gLength, err := readByte(r)
	if err != nil || gLength == 0 || gLength > 32 {
		return srpChallenge{}, fmt.Errorf("invalid SRP generator length %d", gLength)
	}
	g := make([]byte, int(gLength))
	if _, err := io.ReadFull(r, g); err != nil {
		return srpChallenge{}, err
	}
	nLength, err := readByte(r)
	if err != nil || nLength == 0 || nLength > 64 {
		return srpChallenge{}, fmt.Errorf("invalid SRP modulus length %d", nLength)
	}
	n := make([]byte, int(nLength))
	if _, err := io.ReadFull(r, n); err != nil {
		return srpChallenge{}, err
	}
	salt := make([]byte, 32)
	versionChallenge := make([]byte, 16)
	if _, err := io.ReadFull(r, salt); err != nil {
		return srpChallenge{}, err
	}
	if _, err := io.ReadFull(r, versionChallenge); err != nil {
		return srpChallenge{}, err
	}
	securityFlags, err := readByte(r)
	if err != nil {
		return srpChallenge{}, err
	}
	if securityFlags != 0 {
		return srpChallenge{}, fmt.Errorf("unsupported legacy auth security flags 0x%02x", securityFlags)
	}
	return srpChallenge{B: b, G: g, N: n, Salt: salt}, nil
}

func logonProofPacket(proof srpProof) []byte {
	packet := make([]byte, 0, 75)
	packet = append(packet, opLogonProof)
	packet = append(packet, proof.A[:]...)
	packet = append(packet, proof.M1[:]...)
	packet = append(packet, make([]byte, 20)...)
	packet = append(packet, 0, 0)
	return packet
}

func readLogonProof(r io.Reader, expected [20]byte) error {
	opcode, err := readByte(r)
	if err != nil {
		return err
	}
	if opcode != opLogonProof {
		return fmt.Errorf("unexpected auth proof opcode 0x%02x", opcode)
	}
	result, err := readByte(r)
	if err != nil {
		return err
	}
	if ResultCode(result) != ResultSuccess {
		return &AuthError{Code: ResultCode(result)}
	}
	m2 := make([]byte, 20)
	if _, err := io.ReadFull(r, m2); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(m2, expected[:]) != 1 {
		return errors.New("legacy auth returned an invalid SRP server proof")
	}
	trailer := make([]byte, 10) // account flags, survey id, login flags
	_, err = io.ReadFull(r, trailer)
	return err
}

func readRealmList(r io.Reader) ([]Realm, error) {
	opcode, err := readByte(r)
	if err != nil {
		return nil, err
	}
	if opcode != opRealmList {
		return nil, fmt.Errorf("unexpected realm list opcode 0x%02x", opcode)
	}
	var sizeBytes [2]byte
	if _, err := io.ReadFull(r, sizeBytes[:]); err != nil {
		return nil, err
	}
	size := int(binary.LittleEndian.Uint16(sizeBytes[:]))
	if size < 6 || size > 1<<20 {
		return nil, fmt.Errorf("invalid realm list size %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	reader := bytes.NewReader(payload)
	var unused uint32
	var count uint16
	if err := binary.Read(reader, binary.LittleEndian, &unused); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &count); err != nil {
		return nil, err
	}
	realms := make([]Realm, 0, count)
	for i := uint16(0); i < count; i++ {
		realm := Realm{ID: uint32(i + 1)}
		var locked byte
		if err := binary.Read(reader, binary.LittleEndian, &realm.Type); err != nil {
			return nil, err
		}
		if err := binary.Read(reader, binary.LittleEndian, &locked); err != nil {
			return nil, err
		}
		realm.Locked = locked != 0
		if err := binary.Read(reader, binary.LittleEndian, &realm.Flags); err != nil {
			return nil, err
		}
		realm.Name, err = readCString(reader)
		if err != nil {
			return nil, err
		}
		addressAndPort, err := readCString(reader)
		if err != nil {
			return nil, err
		}
		host, port, err := net.SplitHostPort(addressAndPort)
		if err != nil {
			return nil, fmt.Errorf("realm %q address %q: %w", realm.Name, addressAndPort, err)
		}
		parsedPort, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return nil, err
		}
		realm.Address, realm.Port = host, uint16(parsedPort)
		var population uint32
		if err := binary.Read(reader, binary.LittleEndian, &population); err != nil {
			return nil, err
		}
		realm.Population = math.Float32frombits(population)
		if err := binary.Read(reader, binary.LittleEndian, &realm.CharacterCount); err != nil {
			return nil, err
		}
		if err := binary.Read(reader, binary.LittleEndian, &realm.Timezone); err != nil {
			return nil, err
		}
		if _, err := readByte(reader); err != nil {
			return nil, err
		}
		if realm.Flags&0x04 != 0 {
			if err := binary.Read(reader, binary.LittleEndian, &realm.VersionMajor); err != nil {
				return nil, err
			}
			if err := binary.Read(reader, binary.LittleEndian, &realm.VersionMinor); err != nil {
				return nil, err
			}
			if err := binary.Read(reader, binary.LittleEndian, &realm.VersionBugfix); err != nil {
				return nil, err
			}
			if err := binary.Read(reader, binary.LittleEndian, &realm.Build); err != nil {
				return nil, err
			}
		}
		realms = append(realms, realm)
	}
	return realms, nil
}

func readCString(r io.ByteReader) (string, error) {
	var value []byte
	for len(value) <= 4096 {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == 0 {
			return string(value), nil
		}
		value = append(value, b)
	}
	return "", fmt.Errorf("unterminated realm string")
}

func readByte(r io.Reader) (byte, error) {
	var data [1]byte
	_, err := io.ReadFull(r, data[:])
	return data[0], err
}

func reverseASCII(value string) []byte {
	data := []byte(value)
	for left, right := 0, len(data)-1; left < right; left, right = left+1, right-1 {
		data[left], data[right] = data[right], data[left]
	}
	return data
}
