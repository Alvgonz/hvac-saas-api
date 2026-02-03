package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

func newInviteToken() (plain string, hash string, err error) {
	// 32 bytes => token fuerte
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}

	// Token URL-safe
	plain = base64.RawURLEncoding.EncodeToString(b)

	// Guardamos hash hex en DB (nunca el token en claro)
	sum := sha256.Sum256([]byte(plain))
	hash = hex.EncodeToString(sum[:])

	return plain, hash, nil
}
