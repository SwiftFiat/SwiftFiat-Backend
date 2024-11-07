package db

import (
	"context"
	"database/sql"
	"fmt"
)

type Store struct {
	*Queries
	DB *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{
		DB:      db,
		Queries: New(db),
	}
}

func (s *Store) ExecTx(ctx context.Context, fq func(q *Queries) error) error {
	// initialize transaction
	tx, err := s.DB.BeginTx(ctx, nil)
	defer tx.Rollback()

	if err != nil {
		return err
	}

	q := New(tx)
	err = fq(q)

	if err != nil {
		txErr := tx.Rollback()
		if txErr != nil {
			return fmt.Errorf("encountered rollback error: %v", txErr)
		}
		return err
	}

	return tx.Commit()
}
