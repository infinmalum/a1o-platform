package random

import (
	crand "crypto/rand"
	"fmt"
	"math/big"
	"math/rand"
	"strings"

	"github.com/google/uuid"
)

func GetUUID() string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	return code
}

const keyChars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const keyNumbers = "0123456789"

// secureIntn returns a cryptographically secure random int in [0, n). These
// values back API keys, OAuth state, and verification codes, so a time-seeded
// PRNG is not acceptable here.
func secureIntn(n int) int {
	v, err := crand.Int(crand.Reader, big.NewInt(int64(n)))
	if err != nil {
		// Without entropy we cannot mint a secret at all; failing loudly is
		// better than returning a predictable one.
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	return int(v.Int64())
}

func GenerateKey() string {
	key := make([]byte, 48)
	for i := 0; i < 16; i++ {
		key[i] = keyChars[secureIntn(len(keyChars))]
	}
	uuid_ := GetUUID()
	for i := 0; i < 32; i++ {
		c := uuid_[i]
		if i%2 == 0 && c >= 'a' && c <= 'z' {
			c = c - 'a' + 'A'
		}
		key[i+16] = c
	}
	return string(key)
}

func GetRandomString(length int) string {
	key := make([]byte, length)
	for i := 0; i < length; i++ {
		key[i] = keyChars[secureIntn(len(keyChars))]
	}
	return string(key)
}

func GetRandomNumberString(length int) string {
	key := make([]byte, length)
	for i := 0; i < length; i++ {
		key[i] = keyNumbers[secureIntn(len(keyNumbers))]
	}
	return string(key)
}

// RandRange returns a random number between min and max (max is not included).
// Only used for non-security purposes (e.g. channel selection jitter), so the
// math/rand global (auto-seeded since Go 1.20) is fine.
func RandRange(min, max int) int {
	return min + rand.Intn(max-min)
}
