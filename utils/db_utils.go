package utils

import (
	"fmt"
	"math/rand"
	"time"
)

const sslMode = "?sslmode=disable"

func GetDBSource(config *Config, dbName string) string {
	// return the structure "postgres://root:secret@localhost:5432/${db_name}?sslmode=disable"
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s%s", config.DBUsername, config.DBPassword, config.DBHost, config.DBPort, dbName, sslMode)
}

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func GenerateRandomString(length int) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[r.Intn(len(charset))]
	}
	return string(b)
}
