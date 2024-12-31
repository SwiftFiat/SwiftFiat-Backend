package fiat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

type PaystackProvider struct {
	providers.BaseProvider
	config *FiatConfig
}

type FiatConfig struct {
	FiatProviderName    string `mapstructure:"FIAT_PROVIDER_NAME"`
	FiatProviderKey     string `mapstructure:"PAYSTACK_KEY"`
	FiatProviderBaseUrl string `mapstructure:"PAYSTACK_BASE_URL"`
}

func NewFiatProvider() *PaystackProvider {

	var c FiatConfig

	err := utils.LoadCustomConfig(utils.EnvPath, &c)
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	return &PaystackProvider{
		BaseProvider: providers.BaseProvider{
			Name:    c.FiatProviderName,
			BaseURL: c.FiatProviderBaseUrl,
			APIKey:  c.FiatProviderKey,
			Client: &http.Client{
				Timeout: time.Second * 30,
			},
		},
		config: &c,
	}
}

func (p *PaystackProvider) GetBanks() (*BankCollection, error) {
	// This would use the BaseProvider's fields to make the actual HTTP request
	// ...

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	// Path params
	base.Path += "bank"

	resp, err := p.MakeRequest("GET", base.String(), nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		logging.NewLogger().Error("resp", resp)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var banks Response[BankCollection]
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&banks)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &banks.Data, nil
}

func (p *PaystackProvider) ResolveAccount(accountNumber string, bankCode string) (*AccountInfo, error) {
	// This would use the BaseProvider's fields to make the actual HTTP request
	// ...

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	// Path params
	base.Path += "bank/resolve"

	// Query params
	params := url.Values{}
	params.Add("account_number", accountNumber)
	params.Add("bank_code", bankCode)
	base.RawQuery = params.Encode()

	resp, err := p.MakeRequest("GET", base.String(), nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		logging.NewLogger().Error("resp", resp)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var response Response[AccountInfo]
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&response)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &response.Data, nil
}

func (p *PaystackProvider) CreateTransferRecipient(accountNumber string, bankCode string, name string) (*Recipient, error) {
	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	// Path params
	base.Path += "transferrecipient"

	/// Constants are NUBAN and NGN (Naira)
	request := CreateTransferRecipientRequest{
		Type:          "nuban",
		Name:          name,
		AccountNumber: accountNumber,
		BankCode:      bankCode,
		Currency:      "NGN",
	}

	resp, err := p.MakeRequest("POST", base.String(), request, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusCreated {
		logging.NewLogger().Error("resp", resp)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response bodyp
	var response Response[Recipient]
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&response)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &response.Data, nil
}

func (p *PaystackProvider) MakeTransfer(recipient string, amount int64, beneficiaryName string) (*TransferResponse, error) {
	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	// Path params
	base.Path += "transfer"

	/// Constant is Source
	request := TransferRequest{
		Source:    "balance",
		Recipient: recipient,
		Amount:    amount,
		Reason:    fmt.Sprintf("SwiftFiat %v Transfer", beneficiaryName),
	}

	resp, err := p.MakeRequest("POST", base.String(), request, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		logging.NewLogger().Error("resp", resp)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var response Response[TransferResponse]
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&response)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &response.Data, nil
}
