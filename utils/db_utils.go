package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

func GetDBSource(config *Config, dbName string) string {
	sslMode := ""
	if config.SSLMode == "disable" {
		sslMode = "?sslmode=disable"
	} else {
		sslMode = "?sslmode=require"
	}
	// return the structure "postgres://root:secret@localhost:5432/${db_name}?sslmode=disable"
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s%s", config.DBUsername, config.DBPassword, config.DBHost, config.DBPort, dbName, sslMode)
}

const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 32 chars - avoids 0, O, 1, I

// SecureRandomString generates a cryptographically secure random string
// without modulo bias
func SecureRandomString(n int) (string, error) {
	if n <= 0 {
		return "", fmt.Errorf("length must be positive")
	}

	b := make([]byte, n)
	charsetLen := big.NewInt(int64(len(charset)))

	for i := range b {
		// Generate random number without modulo bias
		num, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", fmt.Errorf("failed to generate random number: %w", err)
		}
		b[i] = charset[num.Int64()]
	}

	return string(b), nil
}

// GenerateReferralCode generates a referral code with prefix
// Format: PREFIX-XXXXXXXX (8 random characters)
func GenerateReferralCode(prefix string) (string, error) {
	if prefix == "" {
		return "", fmt.Errorf("prefix cannot be empty")
	}

	randomPart, err := SecureRandomString(8)
	if err != nil {
		return "", fmt.Errorf("failed to generate referral code: %w", err)
	}

	return fmt.Sprintf("%s-%s", strings.ToUpper(prefix), randomPart), nil
}

// MustGenerateReferralCode is like GenerateReferralCode but panics on error
// Use only when you're certain it will succeed (e.g., in tests)
func MustGenerateReferralCode(prefix string) string {
	code, err := GenerateReferralCode(prefix)
	if err != nil {
		panic(fmt.Sprintf("MustGenerateReferralCode: %v", err))
	}
	return code
}
