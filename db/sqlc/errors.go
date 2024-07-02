package db

import "github.com/lib/pq"

const (
	DuplicateEntry pq.ErrorCode = "23505"
	EntryTooLong   pq.ErrorCode = "22001"
)
