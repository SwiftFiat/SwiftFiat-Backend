package utils

import "fmt"

const sslMode = "?sslmode=disable"

func GetDBSource(config *Config, dbName string) string {
	// return the structure "postgres://root:secret@localhost:5432/${db_name}?sslmode=disable"
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s%s", config.DBUsername, config.DBPassword, config.DBHost, config.DBPort, dbName, sslMode)
}
