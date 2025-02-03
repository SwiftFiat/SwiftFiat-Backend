package models

import (
	"strconv"

	"github.com/SwiftFiat/SwiftFiat-Backend/providers/bills"
)

type BillVariation struct {
	VariationCode   string `json:"variation_code"`
	Name            string `json:"name"`
	VariationAmount string `json:"variation_amount"`
	FixedPrice      string `json:"fixed_price"`
}

type ServiceIdentifierResponse struct {
	ServiceID      string  `json:"serviceID"`
	Name           string  `json:"name"`
	MinimiumAmount float64 `json:"minimium_amount"`
	MaximumAmount  float64 `json:"maximum_amount"`
	ConvinienceFee string  `json:"convinience_fee"`
	ProductType    string  `json:"product_type"`
	Image          string  `json:"image"`
}

func ToServiceIdentifierResponse(s bills.ServiceIdentifier) ServiceIdentifierResponse {
	// Parse string amounts to float64
	var minAmount, maxAmount float64
	if str, ok := s.MinimiumAmount.(string); ok {
		minAmount, _ = strconv.ParseFloat(str, 64)
	}
	if str, ok := s.MaximumAmount.(string); ok {
		maxAmount, _ = strconv.ParseFloat(str, 64)
	}

	return ServiceIdentifierResponse{
		ServiceID:      s.ServiceID,
		Name:           s.Name,
		MinimiumAmount: minAmount,
		MaximumAmount:  maxAmount,
		ConvinienceFee: s.ConvinienceFee,
		ProductType:    s.ProductType,
		Image:          s.Image,
	}
}

func ToServiceIdentifierResponseList(s []bills.ServiceIdentifier) []ServiceIdentifierResponse {
	var response []ServiceIdentifierResponse
	for _, service := range s {
		response = append(response, ToServiceIdentifierResponse(service))
	}
	return response
}
