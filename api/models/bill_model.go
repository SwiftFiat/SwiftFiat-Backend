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

func ToMeterInfoResponse(m bills.GetCustomerMeterInfoResponse) MeterInfoResponse {

	var minAmount, minPurchaseAmount, customerArrears float64
	if str, ok := m.MinimumAmount.(string); ok {
		minAmount, _ = strconv.ParseFloat(str, 64)
	}
	if str, ok := m.MinPurchaseAmount.(string); ok {
		minPurchaseAmount, _ = strconv.ParseFloat(str, 64)
	}
	if str, ok := m.CustomerArrears.(string); ok {
		customerArrears, _ = strconv.ParseFloat(str, 64)
	}

	return MeterInfoResponse{
		CustomerName:        m.CustomerName,
		Address:             m.Address,
		MeterNumber:         m.MeterNumber,
		CustomerArrears:     customerArrears,
		MinimumAmount:       minAmount,
		MinPurchaseAmount:   minPurchaseAmount,
		CanVend:             m.CanVend,
		BusinessUnit:        m.BusinessUnit,
		CustomerAccountType: m.CustomerAccountType,
		MeterType:           m.MeterType,
		WrongBillersCode:    m.WrongBillersCode,
	}
}

type MeterInfoResponse struct {
	CustomerName        string  `json:"customer_name"`
	Address             string  `json:"address"`
	MeterNumber         string  `json:"meter_number"`
	CustomerArrears     float64 `json:"customer_arrears"`
	MinimumAmount       float64 `json:"minimum_amount"`
	MinPurchaseAmount   float64 `json:"min_purchase_amount"`
	CanVend             string  `json:"can_vend"`
	BusinessUnit        string  `json:"business_unit"`
	CustomerAccountType string  `json:"customer_account_type"`
	MeterType           string  `json:"meter_type"`
	WrongBillersCode    bool    `json:"wrong_billers_code"`
}
