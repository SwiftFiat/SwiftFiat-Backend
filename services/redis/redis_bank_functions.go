package redis

import (
	"context"
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
)

// StoreBankResponseCollection stores the BankResponseCollection in Redis using hashes
func (r *RedisService) StoreBankResponseCollection(ctx context.Context, key string, collection models.BankResponseCollection) error {
	for _, bank := range collection {
		// Create a unique key for each bank, e.g., using its slug
		bankKey := fmt.Sprintf("%s:%s", key, bank.Slug)

		// Store each field of the bank as a hash
		err := r.client.HSet(ctx, bankKey, bank).Err()
		if err != nil {
			return fmt.Errorf("could not store bank %s in Redis: %w", bank.Slug, err)
		}
	}

	return nil // Return nil if successful
}

// GetBankResponseCollection retrieves the BankResponseCollection from Redis
func (r *RedisService) GetBankResponseCollection(ctx context.Context, key string) (models.BankResponseCollection, error) {
	var collection models.BankResponseCollection

	// Get all keys that match the pattern
	keys, err := r.client.Keys(ctx, fmt.Sprintf("%s:*", key)).Result()
	if err != nil {
		return nil, fmt.Errorf("could not get bank keys from Redis: %w", err)
	}

	var bank models.BankResponse

	for _, bankKey := range keys {
		// Retrieve the hash for each bank
		err := r.client.HGetAll(ctx, bankKey).Scan(&bank)
		if err != nil {
			return nil, fmt.Errorf("could not get bank %s from Redis: %w", bankKey, err)
		}

		collection = append(collection, bank)
	}

	return collection, nil // Return the collection
}
