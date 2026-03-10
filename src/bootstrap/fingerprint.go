package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

func readConfigFingerprint(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
