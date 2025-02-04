package models

import (
	"encoding/json"
	"fmt"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
)

type GiftCardBrandNames struct {
	CountryID   int    `json:"country_id"`
	ProductName string `json:"product_name"`
}

type GiftCardResponse struct {
	ID                       int32           `json:"id"`
	ProductID                int64           `json:"product_id"`
	ProductName              string          `json:"product_name"`
	DenominationType         string          `json:"denomination_type"`
	DiscountPercentage       string          `json:"discount_percentage"`
	MaxRecipientDenomination string          `json:"max_recipient_denomination"`
	MinRecipientDenomination string          `json:"min_recipient_denomination"`
	MaxSenderDenomination    string          `json:"max_sender_denomination"`
	MinSenderDenomination    string          `json:"min_sender_denomination"`
	GiftcardDenominations    json.RawMessage `json:"giftcard_denominations"`
	Global                   string          `json:"global"`
	Metadata                 json.RawMessage `json:"metadata"`
	RecipientCurrencyCode    string          `json:"recipient_currency_code"`
	SenderCurrencyCode       string          `json:"sender_currency_code"`
	SenderFee                string          `json:"sender_fee"`
	SenderFeePercentage      string          `json:"sender_fee_percentage"`
	SupportsPreOrder         string          `json:"supports_pre_order"`
	LogoUrls                 json.RawMessage `json:"logo_urls"`
	BrandName                string          `json:"brand_name"`
	CategoryName             string          `json:"category_name"`
	CountryName              string          `json:"country_name"`
	FlagUrl                  string          `json:"flag_url"`
}

func ToGiftCardBrandNamesResponse(giftCardBrandNames []db.SelectCountriesByBrandIDRow) []GiftCardBrandNames {

	response := []GiftCardBrandNames{}
	for _, brand := range giftCardBrandNames {
		response = append(response, GiftCardBrandNames{
			CountryID:   int(brand.CountryID.Int64),
			ProductName: brand.ProductName.String,
		})
	}
	return response
}

func ToGiftCardResponse(dbVal []db.FetchGiftCardsRow) []*GiftCardResponse {
	giftCardResponses := make([]*GiftCardResponse, len(dbVal))
	for i, val := range dbVal {
		giftCardResponses[i] = &GiftCardResponse{
			ID:                       val.ID,
			ProductID:                val.ProductID,
			ProductName:              val.ProductName.String,
			DenominationType:         val.DenominationType.String,
			DiscountPercentage:       fmt.Sprintf("%f", val.DiscountPercentage.Float64),
			MaxRecipientDenomination: fmt.Sprintf("%f", val.MaxRecipientDenomination.Float64),
			MinRecipientDenomination: fmt.Sprintf("%f", val.MinRecipientDenomination.Float64),
			MaxSenderDenomination:    fmt.Sprintf("%f", val.MaxSenderDenomination.Float64),
			MinSenderDenomination:    fmt.Sprintf("%f", val.MinSenderDenomination.Float64),
			Global:                   fmt.Sprintf("%t", val.Global.Bool),
			Metadata:                 val.Metadata.RawMessage,
			RecipientCurrencyCode:    val.RecipientCurrencyCode.String,
			SenderCurrencyCode:       val.SenderCurrencyCode.String,
			SenderFee:                fmt.Sprintf("%f", val.SenderFee.Float64),
			SenderFeePercentage:      fmt.Sprintf("%f", val.SenderFeePercentage.Float64),
			SupportsPreOrder:         fmt.Sprintf("%t", val.SupportsPreOrder.Bool),
			LogoUrls:                 val.LogoUrls,
			BrandName:                val.BrandName.String,
			CategoryName:             val.CategoryName.String,
			CountryName:              val.CountryName.String,
			FlagUrl:                  val.FlagUrl.String,
			GiftcardDenominations:    val.GiftcardDenominations,
		}
	}
	return giftCardResponses
}

func ToGiftCardSelectGiftCardsByCountryIDAndBrandIDResponse(dbVal []db.SelectGiftCardsByCountryIDAndBrandIDRow) []GiftCardResponse {
	response := []GiftCardResponse{}
	for _, val := range dbVal {
		response = append(response, GiftCardResponse{
			ID:                       val.ID,
			ProductID:                val.ProductID,
			ProductName:              val.ProductName.String,
			DenominationType:         val.DenominationType.String,
			DiscountPercentage:       fmt.Sprintf("%f", val.DiscountPercentage.Float64),
			MaxRecipientDenomination: fmt.Sprintf("%f", val.MaxRecipientDenomination.Float64),
			MinRecipientDenomination: fmt.Sprintf("%f", val.MinRecipientDenomination.Float64),
			MaxSenderDenomination:    fmt.Sprintf("%f", val.MaxSenderDenomination.Float64),
			MinSenderDenomination:    fmt.Sprintf("%f", val.MinSenderDenomination.Float64),
			GiftcardDenominations:    val.GiftcardDenominations,
			Global:                   fmt.Sprintf("%t", val.Global.Bool),
			Metadata:                 val.Metadata.RawMessage,
			RecipientCurrencyCode:    val.RecipientCurrencyCode.String,
			SenderCurrencyCode:       val.SenderCurrencyCode.String,
			SenderFee:                fmt.Sprintf("%f", val.SenderFee.Float64),
			SenderFeePercentage:      fmt.Sprintf("%f", val.SenderFeePercentage.Float64),
			SupportsPreOrder:         fmt.Sprintf("%t", val.SupportsPreOrder.Bool),
			LogoUrls:                 val.LogoUrls,
			BrandName:                val.BrandName.String,
			CategoryName:             val.CategoryName.String,
			CountryName:              val.CountryName.String,
			FlagUrl:                  val.FlagUrl.String,
		})
	}
	return response
}

type GiftCardBrandResponse struct {
	ID            int32  `json:"id"`
	BrandID       int64  `json:"brand_id"`
	BrandName     string `json:"brand_name"`
	BrandLogoUrl  string `json:"brand_logo_url"`
	GiftCardCount int64  `json:"gift_card_count"`
}

func ToGiftCardBrandResponse(dbVal []db.FetchGiftCardsByBrandRow) []GiftCardBrandResponse {
	response := []GiftCardBrandResponse{}
	for _, val := range dbVal {
		response = append(response, GiftCardBrandResponse{
			ID:            val.ID,
			BrandID:       val.BrandID,
			BrandName:     val.BrandName.String,
			BrandLogoUrl:  val.BrandLogoUrl.String,
			GiftCardCount: val.GiftCardCount,
		})
	}

	return response
}
