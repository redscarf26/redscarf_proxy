package modernworld

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"math"
	"net"
	"sync"
)

const (
	CMSGAuthContinuedSession = uint16(14182)
	CMSGPlayerLogin          = uint16(13803)
	SMSGConnectTo            = uint16(12365)
	SMSGResumeComms          = uint16(12363)
	SMSGLoginVerifyWorld     = uint16(9623)
	SMSGCharacterLoginFailed = uint16(9989)

	ConnectToWorldAttempt1 = uint32(17)
	ConnectionTypeInstance = byte(1)
)

var continuedSessionSeed = []byte{0x16, 0xad, 0x0c, 0xd4, 0x46, 0xf9, 0x4f, 0xb2, 0xef, 0x7d, 0xea, 0x2a, 0x17, 0x66, 0x4d, 0x2f}

type AuthContinuedSession struct {
	DOSResponse    uint64
	Key            uint64
	LocalChallenge [16]byte
	Digest         [24]byte
}

type PlayerLogin struct {
	GUID    uint64
	FarClip float32
}

type ConnectToPacket struct {
	AddressType byte
	IP          net.IP
	Port        uint16
	Serial      uint32
	Connection  byte
	Key         uint64
	Signature   [256]byte
}

func ParseAuthContinuedSession(body []byte) (AuthContinuedSession, error) {
	var session AuthContinuedSession
	if len(body) != 56 {
		return session, fmt.Errorf("continued auth session has %d bytes, want 56", len(body))
	}
	session.DOSResponse = binary.LittleEndian.Uint64(body[:8])
	session.Key = binary.LittleEndian.Uint64(body[8:16])
	copy(session.LocalChallenge[:], body[16:32])
	copy(session.Digest[:], body[32:56])
	return session, nil
}

func EncodeAuthContinuedSession(session AuthContinuedSession) []byte {
	body := binary.LittleEndian.AppendUint64(nil, session.DOSResponse)
	body = binary.LittleEndian.AppendUint64(body, session.Key)
	body = append(body, session.LocalChallenge[:]...)
	return append(body, session.Digest[:]...)
}

func DeriveContinuedKeys(sessionKey [40]byte, connectKey uint64, serverChallenge, localChallenge [16]byte) (digest [24]byte, encryptionKey [16]byte) {
	mac := hmac.New(sha256.New, sessionKey[:])
	var encodedKey [8]byte
	binary.LittleEndian.PutUint64(encodedKey[:], connectKey)
	_, _ = mac.Write(encodedKey[:])
	_, _ = mac.Write(localChallenge[:])
	_, _ = mac.Write(serverChallenge[:])
	_, _ = mac.Write(continuedSessionSeed)
	copy(digest[:], mac.Sum(nil)[:24])

	mac = hmac.New(sha256.New, sessionKey[:])
	_, _ = mac.Write(localChallenge[:])
	_, _ = mac.Write(serverChallenge[:])
	_, _ = mac.Write(encryptionKeySeed)
	copy(encryptionKey[:], mac.Sum(nil)[:16])
	return digest, encryptionKey
}

func VerifyContinuedDigest(sessionKey [40]byte, serverChallenge [16]byte, session AuthContinuedSession) bool {
	want, _ := DeriveContinuedKeys(sessionKey, session.Key, serverChallenge, session.LocalChallenge)
	return hmac.Equal(session.Digest[:], want[:])
}

func ParsePlayerLogin(body []byte) (PlayerLogin, error) {
	var login PlayerLogin
	low, _, consumed, err := readPackedGUID128(body)
	if err != nil {
		return login, err
	}
	if consumed+4 != len(body) {
		return login, fmt.Errorf("player-login packet has %d trailing/missing bytes", len(body)-consumed-4)
	}
	login.GUID = uint64(uint32(low))
	login.FarClip = math.Float32frombits(binary.LittleEndian.Uint32(body[consumed:]))
	return login, nil
}

func EncodePlayerLogin(guid uint64, farClip float32) []byte {
	body := appendPackedGUID128(nil, uint64(uint32(guid)), uint64(2)<<58|uint64(1)<<42)
	return binary.LittleEndian.AppendUint32(body, math.Float32bits(farClip))
}

func EncodeConnectTo(address string, port uint16, serial uint32, connectKey uint64) ([]byte, error) {
	ip := net.ParseIP(address)
	if ip == nil {
		return nil, fmt.Errorf("connect-to address %q is not an IP address", address)
	}
	addressType := byte(2)
	addressBytes := ip.To16()
	if ipv4 := ip.To4(); ipv4 != nil {
		addressType = 1
		addressBytes = ipv4
	}
	where := append([]byte{addressType}, addressBytes...)
	digest := connectToDigest(where, addressType, port)
	key, err := connectToPrivateKey()
	if err != nil {
		return nil, err
	}
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return nil, fmt.Errorf("sign connect-to address: %w", err)
	}
	reverseBytes(signature)
	body := append([]byte(nil), signature...)
	body = append(body, where...)
	body = binary.LittleEndian.AppendUint16(body, port)
	body = binary.LittleEndian.AppendUint32(body, serial)
	body = append(body, ConnectionTypeInstance)
	return binary.LittleEndian.AppendUint64(body, connectKey), nil
}

func ParseConnectTo(body []byte) (ConnectToPacket, error) {
	var packet ConnectToPacket
	if len(body) < 256+1+2+4+1+8 {
		return packet, fmt.Errorf("connect-to packet has %d bytes", len(body))
	}
	copy(packet.Signature[:], body[:256])
	position := 256
	packet.AddressType = body[position]
	position++
	addressSize := 0
	switch packet.AddressType {
	case 1:
		addressSize = net.IPv4len
	case 2:
		addressSize = net.IPv6len
	default:
		return packet, fmt.Errorf("unsupported connect-to address type %d", packet.AddressType)
	}
	if position+addressSize+15 != len(body) {
		return packet, fmt.Errorf("connect-to packet size %d does not match address type %d", len(body), packet.AddressType)
	}
	packet.IP = append(net.IP(nil), body[position:position+addressSize]...)
	position += addressSize
	packet.Port = binary.LittleEndian.Uint16(body[position : position+2])
	position += 2
	packet.Serial = binary.LittleEndian.Uint32(body[position : position+4])
	position += 4
	packet.Connection = body[position]
	position++
	packet.Key = binary.LittleEndian.Uint64(body[position : position+8])
	return packet, nil
}

func VerifyConnectTo(body []byte) error {
	packet, err := ParseConnectTo(body)
	if err != nil {
		return err
	}
	addressBytes := packet.IP.To16()
	if packet.AddressType == 1 {
		addressBytes = packet.IP.To4()
	}
	where := append([]byte{packet.AddressType}, addressBytes...)
	digest := connectToDigest(where, packet.AddressType, packet.Port)
	signature := append([]byte(nil), packet.Signature[:]...)
	reverseBytes(signature)
	key, err := connectToPrivateKey()
	if err != nil {
		return err
	}
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		return fmt.Errorf("verify connect-to signature: %w", err)
	}
	return nil
}

func EncodeLoginVerifyWorld(legacyBody []byte) ([]byte, error) {
	if len(legacyBody) != 20 {
		return nil, fmt.Errorf("legacy login-verify-world has %d bytes, want 20", len(legacyBody))
	}
	body := append([]byte(nil), legacyBody...)
	return binary.LittleEndian.AppendUint32(body, 0), nil
}

func EncodeCharacterLoginFailed(legacyBody []byte) ([]byte, error) {
	if len(legacyBody) != 1 {
		return nil, fmt.Errorf("legacy character-login-failed has %d bytes, want 1", len(legacyBody))
	}
	return append([]byte(nil), legacyBody...), nil
}

func connectToDigest(where []byte, addressType byte, port uint16) [32]byte {
	hash := sha256.New()
	_, _ = hash.Write(where)
	var value [4]byte
	binary.LittleEndian.PutUint32(value[:], uint32(addressType))
	_, _ = hash.Write(value[:])
	var encodedPort [2]byte
	binary.LittleEndian.PutUint16(encodedPort[:], port)
	_, _ = hash.Write(encodedPort[:])
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func reverseBytes(value []byte) {
	for left, right := 0, len(value)-1; left < right; left, right = left+1, right-1 {
		value[left], value[right] = value[right], value[left]
	}
}

var (
	connectToRSAOnce sync.Once
	connectToRSAKey  *rsa.PrivateKey
	connectToRSAErr  error
)

func connectToPrivateKey() (*rsa.PrivateKey, error) {
	connectToRSAOnce.Do(func() {
		block, _ := pem.Decode([]byte(connectToPrivateKeyPEM))
		if block == nil {
			connectToRSAErr = fmt.Errorf("decode connect-to RSA private key")
			return
		}
		connectToRSAKey, connectToRSAErr = x509.ParsePKCS1PrivateKey(block.Bytes)
	})
	return connectToRSAKey, connectToRSAErr
}

const connectToPrivateKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA7rPc1NPDtFRRzmZbyzK48PeSU8YZ8gyFL4omqXpFn2DE683q
f41Z2FeyYHsJTJtouMft7x6ADeZrN1tTkOsYEw1/Q2SD2pjmrMIwooKlxsvH+4af
n6kCagNJxTj7wMhVzMDOJZG+hc/R0TfOzIPS6jCAB3uAn51EVCIpvoba20jFqfkT
NpUjdvEO3IQNlAISqJfzOxTuqm+YBSdOH6Ngpana2BffM8viE1SLGLDKubuIZAbf
dabXYQC7sFoOetR3CE0V4hCDsASqnot3qQaJXQhdD7gua8HLZM9uXNtPWGUIUfsN
SBpvtj0fC93+Gx3wv7Ana/WOvMdAAf+nC4DWXwIDAQABAoIBACKa5q/gB2Y0Nyvi
APrDXrZoXclRVd+WWxSaRaKaPE+vuryovI9DUbwgcpa0H5QAj70CFwdsd4oMVozO
6519x56zfTiq8MaXFhIDkQNuR1Q7pMFdMfT2jogJ8/7olO7M3EtzxC8EIwfJKhTX
r15M2h3jbBwplmsNZKOB1GVvrXjOm1KtOZ4CTTM0WrPaLVDT9ax8pykjmFw16vGP
j/R5Dky9VpabtfZOu/AEW259XDEiQgTrB4Eg+S4GJjHqAzPZBmMy/xhlDK4oMXef
qXScfD4w0RxuuCFr6lxLPZz0S35BK1kIWmIkuv+9eQuI4Hr1CyVwch4fkfvrp84x
8tvAFnkCgYEA87NZaG9a8/Mob6GgY4BVLHJVOSzzFdNyMA+4LfSbtzgON2RSZyeD
0JpDowwXssw5XOyUUctj2cLLdlMCpDfdzk4F/PEakloDJWpason3lmur0/5Oq3T9
3+fnNUl4d3UOs1jcJ1yGQ/BfrTyRTcEoZx8Mu9mJ4ituVkKuLeG5vX0CgYEA+r/w
QBJS6kDyQPj1k/SMClUhWhyADwDod03hHTQHc9BleJyjXmVy+/pWhN7aELhjgLbf
o/Gm3aKJjCxS4qBmqUKwAvGoSVux1Bo2ZjcfF7sX9BXBOlFTG+bPVCZUoaksTyXN
g7GsA1frKkWWkgQuOeK3o/p9IZoBl93vEgcTGgsCgYEAv5ucCIjFMllUybCCsrkM
Ps4GQ9YbqmV9ulwhq8BPTlc8lkDCqWhgM3uXAnNXjrUTxQQd+dG4yFZoMrhBs2xZ
cQPXoXDQO5GaN6jPduETUamGiD/DCvwJQCrNlxAVL5dR36FWN3x/9JriHwsoE8Jz
SeEX2frIdpM/RYNX/6sipuECgYEA+rwFRDxOdvm8hGWuQ2WMxyQ7Nn07PEV/LxVM
HkSRkyh23vVakyDEqty3uSOSUJfgv6ud07TnU8ac3fLQatdT8LrDgB4fVkN/fYU8
kldaGwO1vxgl4OfDQCo7dXzisciViwtVBvQZ+jnm6J0vJBFUHAPt9+WZTIlQQIjm
71LtseMCgYBSAhs6lshtz+ujR3fmc4QqJVGqeXvEBPAVm6yYoKYRLwVs/rFv3WLN
LOwwBQ6lz7P9RqYYB5wVlaRvEhb9+lCve/xVcxMeZ5GkOBPxVygYV9l/wNdE25Nz
OHYtKG3GK3GEcFDwZU2LPHq21EroUAdtRfbrJ4KW2yc8igtXKxTBYw==
-----END RSA PRIVATE KEY-----`
