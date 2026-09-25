package legacyauth

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"math/big"
	"testing"
)

func TestComputeProofMatchesServerSideSRP(t *testing.T) {
	username, password := "TESTUSER", "TESTPASS"
	n := littleInt([]byte{
		0x89, 0x4b, 0x64, 0x5e, 0x89, 0xe1, 0x53, 0x5b,
		0xbd, 0xad, 0x5b, 0x8b, 0x29, 0x06, 0x50, 0x53,
		0x08, 0x01, 0xb1, 0x8e, 0xbf, 0xbf, 0x5e, 0x8f,
		0xab, 0x3c, 0x82, 0x87, 0x2a, 0x3e, 0x9b, 0xb7,
	})
	g := big.NewInt(7)
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	identityHash := hashParts([]byte(username + ":" + password))
	xHash := hashParts(salt, identityHash[:])
	x := littleInt(xHash[:])
	verifier := new(big.Int).Exp(g, x, n)
	bPrivate := littleInt(bytes.Repeat([]byte{0x42}, 19))
	gToB := new(big.Int).Exp(g, bPrivate, n)
	serverB := new(big.Int).Add(new(big.Int).Mul(big.NewInt(3), verifier), gToB)
	serverB.Mod(serverB, n)

	challenge := srpChallenge{
		B:    littleBytes(serverB, 32),
		G:    []byte{7},
		N:    littleBytes(n, 32),
		Salt: salt,
	}
	clientRandom := bytes.NewReader(bytes.Repeat([]byte{0x24}, 19))
	proof, err := computeProof(challenge, username, password, clientRandom)
	if err != nil {
		t.Fatal(err)
	}

	clientA := littleInt(proof.A[:])
	uHash := hashParts(proof.A[:], challenge.B)
	u := littleInt(uHash[:])
	serverShared := new(big.Int).Exp(
		new(big.Int).Mul(clientA, new(big.Int).Exp(verifier, u, n)),
		bPrivate,
		n,
	)
	serverKey := interleavedSessionKey(littleBytes(serverShared, 32))
	if subtle.ConstantTimeCompare(serverKey[:], proof.SessionKey[:]) != 1 {
		t.Fatalf("client/server session keys differ")
	}
	nHash := hashParts(challenge.N)
	gHash := hashParts(challenge.G)
	for i := range nHash {
		nHash[i] ^= gHash[i]
	}
	userHash := hashParts([]byte(username))
	wantM1 := hashParts(nHash[:], userHash[:], salt, proof.A[:], challenge.B, serverKey[:])
	if subtle.ConstantTimeCompare(wantM1[:], proof.M1[:]) != 1 {
		t.Fatalf("client proof differs")
	}
	wantM2 := hashParts(proof.A[:], proof.M1[:], serverKey[:])
	if subtle.ConstantTimeCompare(wantM2[:], proof.ExpectedM2[:]) != 1 {
		t.Fatalf("server proof differs")
	}
}

func TestLogonChallengePacketForWotLK(t *testing.T) {
	packet := logonChallengePacket("USER", "zhCN")
	if packet[0] != opLogonChallenge || packet[1] != 8 {
		t.Fatalf("bad challenge prefix: %x", packet[:2])
	}
	if !bytes.Contains(packet, []byte("WoW\x00\x03\x03\x05\x34\x30")) {
		t.Fatalf("3.3.5a build tuple missing: %x", packet)
	}
	if !bytes.Contains(packet, []byte("NChz")) {
		t.Fatalf("reversed locale missing: %x", packet)
	}
}
