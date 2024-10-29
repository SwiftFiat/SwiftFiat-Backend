package apistrings

const (
	UserNotFound              = "user or account does not exist"
	UserDetailsAlreadyCreated = "email or phone number already exists"
	InvalidPhone              = "invalid phone number, please use a standard phone number"
	InvalidEmail              = "invalid email address, please check submitted email address"
	InvalidPhoneEmailInput    = "please enter a valid email and password"
	IncorrectEmailPass        = "incorrect email or password"
	ServerError               = "a server error occurred, please try again later"

	/// KYC Related Strings
	InvalidBVNInput = "invalid bvn input, please check submitted information"
	InvalidNINInput = "invalid nin input, please check submitted information"
	UserNoKYC       = "user does not have KYC information"
)
