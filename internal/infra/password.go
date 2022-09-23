package infra

import (
	"crypto/rand"
	"encoding/hex"
)

const passwordLength = 24

func generatePassword() string {
	b := make([]byte, passwordLength)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
