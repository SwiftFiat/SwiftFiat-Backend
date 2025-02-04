package models

import (
	"slices"
	"strings"

	"github.com/SwiftFiat/SwiftFiat-Backend/providers/fiat"
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
	query = strings.ToLower(query)
	ch := make(chan BankResponse)

	// Goroutine for filtering
	go func() {
		for _, bank := range *c {
			if strings.Contains(strings.ToLower(bank.Name), query) ||
				strings.Contains(strings.ToLower(bank.Slug), query) {
				ch <- bank
			}
		}
		close(ch)
	}()

	var tempBanks BankResponseCollection
	for bank := range ch {
		tempBanks = append(tempBanks, bank)
	}

	// Sort the results by Name
	slices.SortFunc(tempBanks, func(a, b BankResponse) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

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
