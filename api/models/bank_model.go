package models

import (
	"strings"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider/fiat"
)

type BankResponseCollection []BankResponse

type BankResponse struct {
	Name     string `json:"name" redis:"name"`
	Slug     string `json:"slug" redis:"slug"`
	Code     string `json:"code" redis:"code"`
	Longcode string `json:"longcode" redis:"longcode"`
	LogoURL  string `json:"logo_url,omitempty" redis:"logo_url"`
}

func ToBankResponseCollection(banks fiat.BankCollection) BankResponseCollection {

	var tempBanks BankResponseCollection

	for _, bank := range banks {
		bankResponse := ToBankResponseWithLogo(bank, fiat.GetBankLogoByCode(bank.Code))
		tempBanks = append(tempBanks, *bankResponse)
	}
	return tempBanks
}

func (c *BankResponseCollection) FindBanks(query string) *BankResponseCollection {
	var tempBanks BankResponseCollection

	for _, bank := range *c {
		if strings.Contains(strings.ToLower(bank.Name), strings.ToLower(query)) {
			tempBanks = append(tempBanks, bank)
			continue
		}

		if strings.Contains(strings.ToLower(bank.Slug), strings.ToLower(query)) {
			tempBanks = append(tempBanks, bank)
			continue
		}
	}

	/// So as not to show null to the user, we return an empty slice
	if len(tempBanks) == 0 {
		return &BankResponseCollection{}
	}

	return &tempBanks
}

func ToBankResponseWithLogo(bank fiat.Bank, logo string) *BankResponse {
	return &BankResponse{
		Name:     bank.Name,
		Slug:     bank.Slug,
		Code:     bank.Code,
		Longcode: bank.Longcode,
		LogoURL:  logo,
	}
}

type AccountInfoResponse struct {
	AccountName   string `json:"account_name"`
	AccountNumber string `json:"account_number"`
}

func ToAccountInfoResponse(account *fiat.AccountInfo) *AccountInfoResponse {
	return &AccountInfoResponse{
		AccountName:   account.AccountName,
		AccountNumber: account.AccountNumber,
	}
}
