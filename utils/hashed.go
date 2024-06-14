package utils

import "golang.org/x/crypto/bcrypt"

func GenerateHashValue(original string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(original), bcrypt.DefaultCost)

	if err != nil {
		return "", nil
	}

	return string(hash), nil
}

func VerifyHashValue(original, hashedValue string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hashedValue), []byte(original))
	return err
}
