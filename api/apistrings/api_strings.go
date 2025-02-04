package apistrings

const (
	/// Basic User Related Strings
	UserNotFound              = "user or account does not exist"
	UserNotVerified           = "you have not verified your account yet"
	UserDetailsAlreadyCreated = "email or phone number already exists"
	InvalidPhone              = "invalid phone number, please use a standard phone number"
	InvalidEmail              = "invalid email address, please check submitted email address"
	InvalidPhoneEmailInput    = "please enter a valid email and password"
	InvalidCodeEmailInput     = "please enter a valid email and passcode"
	IncorrectEmailPass        = "incorrect email or password"

	/// Core Functionality Error
	ServerError = "a server error occurred, please try again later"

	/// KYC Related Strings
	InvalidBVNInput     = "invalid bvn input, please check submitted information"
	InvalidNINInput     = "invalid nin input, please check submitted information"
	InvalidAddressInput = "invalid address input, please check submitted information"
	UserNoKYC           = "user does not have KYC information"

	/// Wallet Related Strings
	UserNoWallet            = "user does not have a wallet created"
	InvalidCurrency         = "currency entered is not supported"
	DuplicateWallet         = "user already has wallet with currency"
	CurrencyNotSupported    = "entered currency is not supported"
	InvalidWalletInput      = "check 'currency' or 'type' keys, invalid request"
	InvalidTransactionInput = "check 'source_account' or 'amount' keys, invalid request"
	InvalidTransactionID    = "entered ID is invalid"
	InvalidTransactionPIN   = "incorrect PIN, please try again"
)
