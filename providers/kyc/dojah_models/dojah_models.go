package dojahmodels

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
