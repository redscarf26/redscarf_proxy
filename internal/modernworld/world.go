// Package modernworld implements the 3.4.3.54261 world-socket transport and
// the authentication key transition used before opcode translation begins.
package modernworld

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	ClientInitializer = "WORLD OF WARCRAFT CONNECTION - CLIENT TO SERVER - V2\n"
	ServerInitializer = "WORLD OF WARCRAFT CONNECTION - SERVER TO CLIENT - V2\n"

	CMSGAuthSession                      = uint16(14181)
	CMSGEnterEncryptedModeAck            = uint16(14183)
	CMsgPing                             = uint16(14184)
	CMSGLogDisconnect                    = uint16(14185)
	SMSGAuthChallenge                    = uint16(12360)
	SMSGEnterEncryptedMode               = uint16(12361)
	SMsgPong                             = uint16(12366)
	SMSGAuthResponse                     = uint16(9581)
	CMSGEnumCharacters                   = uint16(13801)
	SMSGEnumCharactersResult             = uint16(9603)
	SMSGFeatureSystemStatusGlue          = uint16(9664)
	SMSGSetTimeZoneInformation           = uint16(9847)
	SMSGBattleNetConnectionStatus        = uint16(10249)
	SMSGAvailableHotfixes                = uint16(10511)
	SMSGCacheVersion                     = uint16(10524)
	maxPacketSize                        = 0x40000
	serverNonceSuffix             uint32 = 0x52565253
	clientNonceSuffix             uint32 = 0x544e4c43
)

var (
	authCheckSeed        = []byte{0xc5, 0xc6, 0x98, 0x95, 0x76, 0x3f, 0x1d, 0xcd, 0xb6, 0xa1, 0x37, 0x28, 0xb3, 0x12, 0xff, 0x8a}
	sessionKeySeed       = []byte{0x58, 0xcb, 0xcf, 0x40, 0xfe, 0x2e, 0xce, 0xa6, 0x5a, 0x90, 0xb8, 0x01, 0x68, 0x6c, 0x28, 0x0b}
	encryptionKeySeed    = []byte{0xe9, 0x75, 0x3c, 0x50, 0x90, 0x93, 0x61, 0xda, 0x3b, 0x07, 0xee, 0xfa, 0xff, 0x9d, 0x41, 0xb8}
	enableEncryptionSeed = []byte{0x90, 0x9c, 0xd0, 0x50, 0x5a, 0x2c, 0x14, 0xdd, 0x5c, 0x2c, 0xc0, 0x64, 0x14, 0xf3, 0xfe, 0xc9}
	ed25519Context       = []byte{0xa7, 0x1f, 0xb6, 0x9b, 0xc9, 0x7c, 0xdd, 0x96, 0xe9, 0xbb, 0xb8, 0x21, 0x39, 0x8d, 0x5a, 0xd4}
	ed25519PrivateSeed   = []byte{0x08, 0xbd, 0xc7, 0xa3, 0xcc, 0xc3, 0x4f, 0x3f, 0x6a, 0x0b, 0xff, 0xcf, 0x31, 0xc1, 0xb6, 0x97, 0x69, 0x1e, 0x72, 0x9a, 0x0a, 0xab, 0x2c, 0x77, 0xc3, 0x6f, 0x8a, 0xe7, 0x5a, 0x9a, 0xa7, 0xc9}
)

type Packet struct {
	Opcode uint16
	Body   []byte
}

type AuthChallenge struct {
	DOSChallenge    [32]byte
	ServerChallenge [16]byte
	DOSZeroBits     byte
}

type AuthSession struct {
	DOSResponse     uint64
	RegionID        uint32
	BattlegroupID   uint32
	RealmID         uint32
	LocalChallenge  [16]byte
	Digest          [24]byte
	UseIPv6         bool
	RealmJoinTicket string
}

type PacketConn struct {
	history        packetHistory
	conn           net.Conn
	writeMu        sync.Mutex
	outboundSuffix uint32
	inboundSuffix  uint32
	outboundCount  uint64
	inboundCount   uint64
	aead           cipher.AEAD
}

func NewServerConn(conn net.Conn) *PacketConn {
	return newPacketConn(conn, serverNonceSuffix, clientNonceSuffix)
}

// NewClientConn is primarily useful for protocol-level integration tests and
// golden-flow capture tools.
func NewClientConn(conn net.Conn) *PacketConn {
	return newPacketConn(conn, clientNonceSuffix, serverNonceSuffix)
}

func newPacketConn(conn net.Conn, outboundSuffix, inboundSuffix uint32) *PacketConn {
	return &PacketConn{conn: conn, outboundSuffix: outboundSuffix, inboundSuffix: inboundSuffix}
}

func (c *PacketConn) Close() error {
	return c.conn.Close()
}

func (c *PacketConn) Accept() (AuthChallenge, error) {
	var challenge AuthChallenge
	if err := writeAll(c.conn, []byte(ServerInitializer)); err != nil {
		return challenge, err
	}
	initializer := make([]byte, len(ClientInitializer))
	if _, err := io.ReadFull(c.conn, initializer); err != nil {
		return challenge, err
	}
	if string(initializer) != ClientInitializer {
		return challenge, fmt.Errorf("invalid modern world client initializer")
	}
	if _, err := rand.Read(challenge.DOSChallenge[:]); err != nil {
		return challenge, err
	}
	if _, err := rand.Read(challenge.ServerChallenge[:]); err != nil {
		return challenge, err
	}
	challenge.DOSZeroBits = 1
	body := make([]byte, 49)
	copy(body[:32], challenge.DOSChallenge[:])
	copy(body[32:48], challenge.ServerChallenge[:])
	body[48] = challenge.DOSZeroBits
	if err := c.WritePacket(SMSGAuthChallenge, body); err != nil {
		return challenge, err
	}
	return challenge, nil
}

func (c *PacketConn) EnableEncryption(key []byte) error {
	if c.aead != nil {
		return fmt.Errorf("modern world encryption is already enabled")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	c.aead, err = cipher.NewGCMWithTagSize(block, 12)
	return err
}

func (c *PacketConn) ReadPacket() (Packet, error) {
	header := make([]byte, 16)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return Packet{}, err
	}
	size := int(int32(binary.LittleEndian.Uint32(header[:4])))
	if size < 2 || size >= maxPacketSize {
		return Packet{}, fmt.Errorf("invalid modern world packet size %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return Packet{}, err
	}
	if c.aead != nil {
		nonce := packetNonce(c.inboundCount, c.inboundSuffix)
		sealed := make([]byte, 0, len(payload)+12)
		sealed = append(sealed, payload...)
		sealed = append(sealed, header[4:16]...)
		plain, err := c.aead.Open(nil, nonce[:], sealed, nil)
		if err != nil {
			return Packet{}, fmt.Errorf("decrypt modern world packet: %w", err)
		}
		payload = plain
	}
	c.inboundCount++
	return Packet{Opcode: binary.LittleEndian.Uint16(payload[:2]), Body: payload[2:]}, nil
}

func (c *PacketConn) WritePacket(opcode uint16, body []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	payload := make([]byte, 2+len(body))
	binary.LittleEndian.PutUint16(payload[:2], opcode)
	copy(payload[2:], body)
	header := make([]byte, 16)
	binary.LittleEndian.PutUint32(header[:4], uint32(len(payload)))
	if c.aead != nil {
		nonce := packetNonce(c.outboundCount, c.outboundSuffix)
		sealed := c.aead.Seal(nil, nonce[:], payload, nil)
		copy(payload, sealed[:len(payload)])
		copy(header[4:16], sealed[len(payload):])
	}
	c.outboundCount++
	if err := writeAll(c.conn, header); err != nil {
		return err
	}
	if err := writeAll(c.conn, payload); err != nil {
		return err
	}
	c.history.record(opcode, body)
	return nil
}

func ParseAuthSession(body []byte) (AuthSession, error) {
	var session AuthSession
	const fixed = 8 + 12 + 16 + 24 + 1 + 4
	if len(body) < fixed {
		return session, fmt.Errorf("modern auth session has %d bytes, want at least %d", len(body), fixed)
	}
	position := 0
	session.DOSResponse = binary.LittleEndian.Uint64(body[position : position+8])
	position += 8
	session.RegionID = binary.LittleEndian.Uint32(body[position : position+4])
	position += 4
	session.BattlegroupID = binary.LittleEndian.Uint32(body[position : position+4])
	position += 4
	session.RealmID = binary.LittleEndian.Uint32(body[position : position+4])
	position += 4
	copy(session.LocalChallenge[:], body[position:position+16])
	position += 16
	copy(session.Digest[:], body[position:position+24])
	position += 24
	session.UseIPv6 = body[position]&0x80 != 0
	position++
	ticketSize := int(binary.LittleEndian.Uint32(body[position : position+4]))
	position += 4
	if ticketSize < 0 || ticketSize > len(body)-position {
		return session, fmt.Errorf("modern auth ticket size %d exceeds remaining %d", ticketSize, len(body)-position)
	}
	session.RealmJoinTicket = string(body[position : position+ticketSize])
	if session.RealmJoinTicket == "" {
		return session, fmt.Errorf("modern auth ticket is empty")
	}
	return session, nil
}

func EncodeAuthSession(session AuthSession) []byte {
	body := make([]byte, 0, 8+12+16+24+1+4+len(session.RealmJoinTicket))
	body = binary.LittleEndian.AppendUint64(body, session.DOSResponse)
	body = binary.LittleEndian.AppendUint32(body, session.RegionID)
	body = binary.LittleEndian.AppendUint32(body, session.BattlegroupID)
	body = binary.LittleEndian.AppendUint32(body, session.RealmID)
	body = append(body, session.LocalChallenge[:]...)
	body = append(body, session.Digest[:]...)
	if session.UseIPv6 {
		body = append(body, 0x80)
	} else {
		body = append(body, 0)
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(len(session.RealmJoinTicket)))
	return append(body, session.RealmJoinTicket...)
}

func DeriveKeys(worldKey [64]byte, serverChallenge, localChallenge [16]byte) (sessionKey [40]byte, encryptionKey [16]byte) {
	keyHash := sha256.Sum256(worldKey[:])
	mac := hmac.New(sha256.New, keyHash[:])
	_, _ = mac.Write(serverChallenge[:])
	_, _ = mac.Write(localChallenge[:])
	_, _ = mac.Write(sessionKeySeed)
	generator := newSessionKeyGenerator(mac.Sum(nil))
	generator.generate(sessionKey[:])

	mac = hmac.New(sha256.New, sessionKey[:])
	_, _ = mac.Write(localChallenge[:])
	_, _ = mac.Write(serverChallenge[:])
	_, _ = mac.Write(encryptionKeySeed)
	copy(encryptionKey[:], mac.Sum(nil)[:16])
	return sessionKey, encryptionKey
}

func VerifyAuthDigest(worldKey [64]byte, serverChallenge [16]byte, session AuthSession, buildSeed [16]byte) bool {
	hash := sha256.New()
	_, _ = hash.Write(worldKey[:])
	_, _ = hash.Write(buildSeed[:])
	mac := hmac.New(sha256.New, hash.Sum(nil))
	_, _ = mac.Write(session.LocalChallenge[:])
	_, _ = mac.Write(serverChallenge[:])
	_, _ = mac.Write(authCheckSeed)
	return hmac.Equal(session.Digest[:], mac.Sum(nil)[:24])
}

func EncodeEnterEncryptedMode(encryptionKey [16]byte) ([]byte, error) {
	mac := hmac.New(sha256.New, encryptionKey[:])
	_, _ = mac.Write([]byte{1})
	_, _ = mac.Write(enableEncryptionSeed)
	toSign := mac.Sum(nil)
	privateKey := ed25519.NewKeyFromSeed(ed25519PrivateSeed)
	signature, err := privateKey.Sign(nil, toSign, &ed25519.Options{Hash: crypto.Hash(0), Context: string(ed25519Context)})
	if err != nil {
		return nil, err
	}
	body := append([]byte(nil), signature...)
	return append(body, 0x80), nil
}

func VerifyEnterEncryptedMode(body []byte, encryptionKey [16]byte) error {
	if len(body) != ed25519.SignatureSize+1 || body[len(body)-1]&0x80 == 0 {
		return fmt.Errorf("invalid enter-encrypted-mode body")
	}
	mac := hmac.New(sha256.New, encryptionKey[:])
	_, _ = mac.Write([]byte{1})
	_, _ = mac.Write(enableEncryptionSeed)
	publicKey := ed25519.NewKeyFromSeed(ed25519PrivateSeed).Public().(ed25519.PublicKey)
	return ed25519.VerifyWithOptions(publicKey, mac.Sum(nil), body[:ed25519.SignatureSize], &ed25519.Options{Hash: crypto.Hash(0), Context: string(ed25519Context)})
}

// EncodeAuthResponse emits the smallest successful 3.4.3 auth response: one
// virtual realm, no queue, no templates, and no optional player counts.
func EncodeAuthResponse(realmAddress uint32, realmName string, now time.Time) []byte {
	type raceAvailability struct {
		raceID  byte
		classes []byte
	}
	available := []raceAvailability{
		{1, []byte{1, 2, 4, 5, 8, 9}},
		{2, []byte{1, 3, 4, 7, 9}},
		{3, []byte{1, 2, 3, 4, 5}},
		{4, []byte{1, 3, 4, 5, 11}},
		{5, []byte{1, 4, 5, 8, 9}},
		{6, []byte{1, 3, 7, 11}},
		{7, []byte{1, 4, 8, 9}},
		{8, []byte{1, 3, 4, 5, 7, 8}},
		{10, []byte{2, 3, 4, 5, 8, 9}},
		{11, []byte{1, 2, 3, 5, 7, 8}},
	}
	var body []byte
	body = binary.LittleEndian.AppendUint32(body, 0) // BattlenetRpcErrorCode.Ok
	bits := newBitWriter(body)
	bits.writeBit(true)  // success info
	bits.writeBit(false) // wait info
	body = bits.flush()

	body = binary.LittleEndian.AppendUint32(body, realmAddress)
	body = binary.LittleEndian.AppendUint32(body, 1) // virtual realm count
	body = binary.LittleEndian.AppendUint32(body, 0) // rested time
	body = append(body, 2, 2)                        // active/account WotLK expansion
	body = binary.LittleEndian.AppendUint32(body, 0) // seconds until parental-control kick
	body = binary.LittleEndian.AppendUint32(body, uint32(len(available)))
	body = binary.LittleEndian.AppendUint32(body, 0) // template count
	body = binary.LittleEndian.AppendUint32(body, 0) // currency ID
	body = binary.LittleEndian.AppendUint64(body, uint64(now.Unix()))
	for _, race := range available {
		body = append(body, race.raceID)
		body = binary.LittleEndian.AppendUint32(body, uint32(len(race.classes)+1))
		for _, classID := range race.classes {
			body = append(body, classID, 0, 0, 0)
		}
		// Death Knight is available to every WotLK race and requires WotLK as
		// both the active and minimum active expansion.
		body = append(body, 6, 2, 0, 2)
	}

	bits = newBitWriter(body)
	for range 5 { // trial/template + three optional fields
		bits.writeBit(false)
	}
	body = bits.flush()
	body = binary.LittleEndian.AppendUint32(body, 0) // billing plan
	body = binary.LittleEndian.AppendUint32(body, 0) // time remaining
	body = binary.LittleEndian.AppendUint32(body, 0) // unknown735
	bits = newBitWriter(body)
	bits.writeBit(false)
	bits.writeBit(false)
	bits.writeBit(false)
	body = bits.flush()

	normalized := strings.ReplaceAll(realmName, " ", "")
	if len(realmName) > 255 {
		realmName = realmName[:255]
	}
	if len(normalized) > 255 {
		normalized = normalized[:255]
	}
	body = binary.LittleEndian.AppendUint32(body, realmAddress)
	bits = newBitWriter(body)
	bits.writeBit(true)  // home realm
	bits.writeBit(false) // not internal
	bits.writeBits(uint32(len(realmName)), 8)
	bits.writeBits(uint32(len(normalized)), 8)
	body = bits.flush()
	body = append(body, realmName...)
	body = append(body, normalized...)
	return body
}

type sessionKeyGenerator struct {
	o0    [32]byte
	o1    [32]byte
	o2    [32]byte
	taken int
}

func newSessionKeyGenerator(seed []byte) *sessionKeyGenerator {
	half := len(seed) / 2
	generator := &sessionKeyGenerator{}
	generator.o1 = sha256.Sum256(seed[:half])
	generator.o2 = sha256.Sum256(seed[half:])
	generator.fill()
	return generator
}

func (g *sessionKeyGenerator) fill() {
	hash := sha256.New()
	_, _ = hash.Write(g.o1[:])
	_, _ = hash.Write(g.o0[:])
	_, _ = hash.Write(g.o2[:])
	copy(g.o0[:], hash.Sum(nil))
	g.taken = 0
}

func (g *sessionKeyGenerator) generate(destination []byte) {
	for index := range destination {
		if g.taken == len(g.o0) {
			g.fill()
		}
		destination[index] = g.o0[g.taken]
		g.taken++
	}
}

func packetNonce(counter uint64, suffix uint32) [12]byte {
	var nonce [12]byte
	binary.LittleEndian.PutUint64(nonce[:8], counter)
	binary.LittleEndian.PutUint32(nonce[8:], suffix)
	return nonce
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := w.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

type bitWriter struct {
	data      []byte
	value     byte
	remaining uint8
}

func newBitWriter(data []byte) *bitWriter {
	return &bitWriter{data: data, remaining: 8}
}

func (w *bitWriter) writeBit(value bool) {
	w.remaining--
	if value {
		w.value |= 1 << w.remaining
	}
	if w.remaining == 0 {
		w.data = append(w.data, w.value)
		w.value = 0
		w.remaining = 8
	}
}

func (w *bitWriter) writeBits(value uint32, count int) {
	for bit := count - 1; bit >= 0; bit-- {
		w.writeBit(value&(1<<bit) != 0)
	}
}

func (w *bitWriter) flush() []byte {
	if w.remaining != 8 {
		w.data = append(w.data, w.value)
		w.value = 0
		w.remaining = 8
	}
	return w.data
}
