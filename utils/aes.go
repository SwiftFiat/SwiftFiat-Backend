// Package aes256 provides AES-256 encryption compatible with AES-Everywhere format
// This is compatible with BridgeCard's encryption requirements
package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// Encrypt encrypts plaintext using AES-256-CBC with OpenSSL-compatible format
// Compatible with mervick/aes-everywhere encryption
func Encrypt(plaintext, passphrase string) string {
	// Generate random salt (8 bytes)
	salt := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		panic(err)
	}

	// Derive key and IV using OpenSSL's EVP_BytesToKey equivalent
	key, iv := deriveKeyAndIV([]byte(passphrase), salt)

	// Encrypt using AES-256-CBC
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}

	// Apply PKCS7 padding
	paddedData := pkcs7Pad([]byte(plaintext), aes.BlockSize)

	// Create cipher
	mode := cipher.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, len(paddedData))
	mode.CryptBlocks(ciphertext, paddedData)

	// Prepend "Salted__" + salt to ciphertext (OpenSSL format)
	result := make([]byte, 16+len(ciphertext))
	copy(result[0:8], []byte("Salted__"))
	copy(result[8:16], salt)
	copy(result[16:], ciphertext)

	// Return base64 encoded result
	return base64.StdEncoding.EncodeToString(result)
}

// Decrypt decrypts base64-encoded ciphertext using AES-256-CBC
func Decrypt(ciphertext, passphrase string) string {
	// Decode base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return ""
	}

	// Check for "Salted__" prefix
	if len(data) < 16 || string(data[0:8]) != "Salted__" {
		return ""
	}

	// Extract salt and ciphertext
	salt := data[8:16]
	encrypted := data[16:]

	// Derive key and IV
	key, iv := deriveKeyAndIV([]byte(passphrase), salt)

	// Decrypt using AES-256-CBC
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}

	if len(encrypted)%aes.BlockSize != 0 {
		return ""
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(encrypted))
	mode.CryptBlocks(plaintext, encrypted)

	// Remove PKCS7 padding
	unpaddedData, err := pkcs7Unpad(plaintext, aes.BlockSize)
	if err != nil {
		return ""
	}

	return string(unpaddedData)
}

// deriveKeyAndIV derives a 32-byte key and 16-byte IV from passphrase and salt
// This mimics OpenSSL's EVP_BytesToKey with MD5 hash
func deriveKeyAndIV(passphrase, salt []byte) ([]byte, []byte) {
	var (
		key      = make([]byte, 32) // AES-256 requires 32-byte key
		iv       = make([]byte, 16) // AES requires 16-byte IV
		keyIV    = make([]byte, 48) // Combined key + IV
		prevHash []byte
	)

	// Iteratively hash to generate enough bytes
	for len(keyIV) < 48 {
		h := md5.New()
		if len(prevHash) > 0 {
			h.Write(prevHash)
		}
		h.Write(passphrase)
		h.Write(salt)
		prevHash = h.Sum(nil)
		copy(keyIV[len(keyIV)-len(prevHash):], prevHash)

		// Prevent infinite loop
		if len(prevHash) == 0 {
			break
		}
	}

	// Generate key and IV by hashing multiple times
	h1 := md5.Sum(append(append(passphrase, salt...), []byte{}...))
	h2 := md5.Sum(append(append(h1[:], passphrase...), salt...))
	h3 := md5.Sum(append(append(h2[:], passphrase...), salt...))

	copy(key, h1[:])
	copy(key[16:], h2[:])
	copy(iv, h3[:])

	return key, iv
}

// pkcs7Pad applies PKCS7 padding to data
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	padText := make([]byte, padding)
	for i := range padText {
		padText[i] = byte(padding)
	}
	return append(data, padText...)
}

// pkcs7Unpad removes PKCS7 padding from data
func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}
	if len(data)%blockSize != 0 {
		return nil, errors.New("invalid padding size")
	}

	padding := int(data[len(data)-1])
	if padding > blockSize || padding == 0 {
		return nil, errors.New("invalid padding")
	}

	// Verify padding
	for i := 0; i < padding; i++ {
		if data[len(data)-1-i] != byte(padding) {
			return nil, errors.New("invalid padding bytes")
		}
	}

	return data[:len(data)-padding], nil
}
