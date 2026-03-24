package dojahmodels

import (
	"encoding/json"
	"fmt"
)

type BVNResponse struct {
	Entity BVNEntity `json:"entity"`
}

type BVNEntity struct {
	BVN       EntityInfo `json:"bvn"`
	FirstName EntityInfo `json:"first_name"`
	LastName  EntityInfo `json:"last_name"`
	DOB       EntityInfo `json:"date_of_birth"`
}

type EntityInfo struct {
	ConfidenceValue int    `json:"confidence_value"`
	Value           string `json:"value"`
	Status          bool   `json:"status"`
}

func (e *EntityInfo) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as the full object first
	type alias EntityInfo
	var a alias
	if err := json.Unmarshal(data, &a); err == nil {
		*e = EntityInfo(a)
		return nil
	}

	// If that fails, try to unmarshal as a simple string
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		e.Value = s
		e.Status = s != ""
		return nil
	}

	return fmt.Errorf("failed to unmarshal EntityInfo: %s", string(data))
}

type SelfieVerification struct {
	ConfidenceValue float64 `json:"confidence_value"`
	Match           bool    `json:"match"`
}

type NINResponse struct {
	Entity NINEntity `json:"entity"`
}

// Define the struct for the main JSON object
type NINEntity struct {
	FirstName          string             `json:"first_name"`
	LastName           string             `json:"last_name"`
	MiddleName         string             `json:"middle_name"`
	Gender             string             `json:"gender"`
	Image              string             `json:"image"`
	PhoneNumber        string             `json:"phone_number"`
	DateOfBirth        string             `json:"date_of_birth"`
	NIN                string             `json:"nin"`
	SelfieVerification SelfieVerification `json:"selfie_verification"`
}

type UtilityBillResponse struct {
	Entity UtilityBillEntity `json:"entity"`
}

type UtilityBillEntity struct {
	Result        UtilityBillResult   `json:"result"`
	IdentityInfo  UtilityBillIdentity `json:"identity_info"`
	AddressInfo   UtilityBillAddress  `json:"address_info"`
	ProviderName  string              `json:"provider_name"`
	BillIssueDate string              `json:"bill_issue_date"`
	AmountPaid    string              `json:"amount_paid"`
	Metadata      UtilityBillMetadata `json:"metadata"`
}

type UtilityBillResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type UtilityBillIdentity struct {
	FullName    string `json:"full_name"`
	MeterNumber string `json:"meter_number"`
}

type UtilityBillAddress struct {
	Street  string `json:"street"`
	City    string `json:"city"`
	State   string `json:"state"`
	Country string `json:"country"`
}

type UtilityBillMetadata struct {
	ExtractionDate string `json:"extraction_date"`
	IsRecent       bool   `json:"is_recent"`
}
