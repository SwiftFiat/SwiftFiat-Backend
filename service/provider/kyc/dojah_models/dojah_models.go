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

type Response struct {
	Entity BVNEntity `json:"entity"`
}
