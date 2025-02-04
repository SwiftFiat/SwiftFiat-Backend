package utils

import (
	"fmt"
	"math/rand"
	"strconv"
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

func GenerateRandomString(prefix string, userID int64, firstName string, lastName string) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	randomChars := make([]byte, 6)
	for i := range randomChars {
		randomChars[i] = charset[rand.Intn(len(charset))]
	}

	randomString := strings.ToUpper(prefix + firstName[:3] + lastName[:3] + strconv.FormatInt(userID, 10) + string(randomChars))
	return randomString
}
