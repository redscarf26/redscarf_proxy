package legacyauth

import (
	"crypto/sha1" // WoW 3.3.5a SRP6 uses SHA-1 by protocol definition.
	"fmt"
	"io"
	"math/big"
	"strings"
)

type srpChallenge struct {
	B    []byte
	G    []byte
	N    []byte
	Salt []byte
}

type srpProof struct {
	A          [32]byte
	M1         [20]byte
	ExpectedM2 [20]byte
	SessionKey [40]byte
}

func computeProof(challenge srpChallenge, username, password string, random io.Reader) (srpProof, error) {
	if len(challenge.B) != 32 || len(challenge.N) != 32 || len(challenge.Salt) != 32 || len(challenge.G) == 0 {
		return srpProof{}, fmt.Errorf("invalid SRP challenge lengths B=%d g=%d N=%d salt=%d", len(challenge.B), len(challenge.G), len(challenge.N), len(challenge.Salt))
	}
	n := littleInt(challenge.N)
	g := littleInt(challenge.G)
	b := littleInt(challenge.B)
	if n.Sign() <= 0 || g.Sign() <= 0 || b.Sign() <= 0 {
		return srpProof{}, fmt.Errorf("invalid zero SRP parameter")
	}

	identityHash := hashParts([]byte(strings.ToUpper(username + ":" + password)))
	xHash := hashParts(challenge.Salt, identityHash[:])
	x := littleInt(xHash[:])

	privateBytes := make([]byte, 19)
	if _, err := io.ReadFull(random, privateBytes); err != nil {
		return srpProof{}, fmt.Errorf("generate SRP private value: %w", err)
	}
	a := littleInt(privateBytes)
	if a.Sign() == 0 {
		a.SetInt64(1)
	}

	A := new(big.Int).Exp(g, a, n)
	if A.Sign() == 0 {
		return srpProof{}, fmt.Errorf("invalid SRP public value")
	}
	aBytes := littleBytes(A, 32)
	uHash := hashParts(aBytes, challenge.B)
	u := littleInt(uHash[:])

	gToX := new(big.Int).Exp(g, x, n)
	kgx := new(big.Int).Mul(big.NewInt(3), gToX)
	base := new(big.Int).Sub(b, kgx)
	base.Mod(base, n)
	if base.Sign() < 0 {
		base.Add(base, n)
	}
	exponent := new(big.Int).Add(a, new(big.Int).Mul(u, x))
	sharedSecret := new(big.Int).Exp(base, exponent, n)
	sharedBytes := littleBytes(sharedSecret, 32)
	sessionKey := interleavedSessionKey(sharedBytes)

	nHash := hashParts(challenge.N)
	gHash := hashParts(challenge.G)
	for i := range nHash {
		nHash[i] ^= gHash[i]
	}
	userHash := hashParts([]byte(strings.ToUpper(username)))
	m1 := hashParts(nHash[:], userHash[:], challenge.Salt, aBytes, challenge.B, sessionKey[:])
	m2 := hashParts(aBytes, m1[:], sessionKey[:])

	var proof srpProof
	copy(proof.A[:], aBytes)
	proof.M1 = m1
	proof.ExpectedM2 = m2
	proof.SessionKey = sessionKey
	return proof, nil
}

func interleavedSessionKey(shared []byte) [40]byte {
	var even, odd [16]byte
	for i := 0; i < 16; i++ {
		even[i] = shared[i*2]
		odd[i] = shared[i*2+1]
	}
	evenHash := hashParts(even[:])
	oddHash := hashParts(odd[:])
	var key [40]byte
	for i := 0; i < 20; i++ {
		key[i*2] = evenHash[i]
		key[i*2+1] = oddHash[i]
	}
	return key
}

func hashParts(parts ...[]byte) [20]byte {
	h := sha1.New()
	for _, part := range parts {
		_, _ = h.Write(part)
	}
	var result [20]byte
	copy(result[:], h.Sum(nil))
	return result
}

func littleInt(data []byte) *big.Int {
	reversed := make([]byte, len(data))
	for i := range data {
		reversed[len(data)-1-i] = data[i]
	}
	return new(big.Int).SetBytes(reversed)
}

func littleBytes(value *big.Int, size int) []byte {
	bigEndian := value.Bytes()
	if len(bigEndian) > size {
		bigEndian = bigEndian[len(bigEndian)-size:]
	}
	result := make([]byte, size)
	for i := range bigEndian {
		result[i] = bigEndian[len(bigEndian)-1-i]
	}
	return result
}
