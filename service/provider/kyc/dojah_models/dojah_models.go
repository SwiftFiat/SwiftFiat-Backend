package dojahmodels

type BVNEntity struct {
	BVN          string `json:"bvn"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	MiddleName   string `json:"middle_name"`
	Gender       string `json:"gender"`
	DateOfBirth  string `json:"date_of_birth"`
	PhoneNumber1 string `json:"phone_number1"`
	Image        string `json:"image"`
	PhoneNumber2 string `json:"phone_number2"`
}

type SelfieVerification struct {
	ConfidenceValue float64 `json:"confidence_value"`
	Match           bool    `json:"match"`
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

type NINResponse struct {
	Entity NINEntity `json:"entity"`
}

type BVNResponse struct {
	Entity BVNEntity `json:"entity"`
}
