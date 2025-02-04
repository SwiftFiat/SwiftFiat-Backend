package redis

import (
	"context"
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
)

func (r *RedisService) StoreVariations(ctx context.Context, key string, variations []models.BillVariation) error {
	for _, variation := range variations {
		// Create a unique key for each variation
		variationKey := fmt.Sprintf("%s:%s", key, variation.VariationCode)

		// Store each field of the variation as a hash
		err := r.client.HSet(ctx, variationKey, map[string]interface{}{
			"variation_code":   variation.VariationCode,
			"name":             variation.Name,
			"variation_amount": variation.VariationAmount,
			"fixed_price":      variation.FixedPrice,
		}).Err()
		if err != nil {
			return fmt.Errorf("failed to store variation %s: %w", variation.VariationCode, err)
		}
	}
	return nil
}

func (r *RedisService) GetVariations(ctx context.Context, key string) ([]models.BillVariation, error) {
	// Get all keys that match the pattern
	keys, err := r.client.Keys(ctx, fmt.Sprintf("%s:*", key)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get variation keys: %w", err)
	}

	var variations []models.BillVariation

	for _, variationKey := range keys {
		// Get the hash fields for this variation
		fields, err := r.client.HGetAll(ctx, variationKey).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get variation %s: %w", variationKey, err)
		}

		// Create BillVariation from hash fields
		variation := models.BillVariation{
			VariationCode:   fields["variation_code"],
			Name:            fields["name"],
			VariationAmount: fields["variation_amount"],
			FixedPrice:      fields["fixed_price"],
		}

		variations = append(variations, variation)
	}

	return variations, nil
}
