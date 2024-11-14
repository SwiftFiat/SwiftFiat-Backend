package reloadlymodels

import (
	"fmt"
)

// ParseIntoBrandCollection processes the GiftCardCollection and populates the BrandCollection.
func ParseIntoBrandCollection(products GiftCardCollection, brand_collection *BrandCollection) error {
	// Ensure the output brand_collection is initialized.
	// This is important to avoid appending to a nil slice.
	if brand_collection == nil {
		return fmt.Errorf("brand_collection is nil")
	}
	if *brand_collection == nil {
		*brand_collection = []BrandElement{}
	}

	// Iterate over the GiftCardCollection to extract brand data.
	for _, product := range products {
		if product.Brand != nil {
			// Check if the brand already exists in the BrandCollection
			exists := false
			for _, brand := range *brand_collection {
				if brand.BrandName == *product.Brand.BrandName {
					exists = true
					break
				}
			}

			// If the brand does not exist, add it to the collection.
			if !exists {
				*brand_collection = append(*brand_collection, BrandElement{
					BrandName: *product.Brand.BrandName,
					ID:        int(*product.Brand.BrandID), // Assuming BrandID is a pointer to int64
					LogoURL:   product.LogoUrls[0],         // Assuming the first logo URL is the one to use
				})
			}
		}
	}

	return nil
}
