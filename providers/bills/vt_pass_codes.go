package bills

// VTPass response codes and their meanings
const (
	// TransactionProcessed - Transaction is processed. Check [content][transactions][status] for actual state
	TransactionProcessed = "000"
	// TransactionProcessing - Transaction is currently processing. Requery using requestID to check status
	TransactionProcessing = "099"
	// TransactionQuery - Current status of a transaction on the platform
	TransactionQuery = "001"
	// TransactionResolved - Transaction has been resolved. Contact support for more info
	TransactionResolved = "044"
	// TransactionNotProcessed - Transaction not processed and no charge applied
	TransactionNotProcessed = "091"
	// TransactionFailed - Transaction failed
	TransactionFailed = "016"
	// InvalidVariationCode - Invalid variation code used
	InvalidVariationCode = "010"
	// InvalidArguments - Missing required arguments in request
	InvalidArguments = "011"
	// ProductNotExist - Product does not exist
	ProductNotExist = "012"
	// BelowMinAmount - Amount is below minimum allowed for product/service
	BelowMinAmount = "013"
	// RequestIDExists - RequestID was already used in previous transaction
	RequestIDExists = "014"
	// InvalidRequestID - RequestID not found for requery operation
	InvalidRequestID = "015"
	// AboveMaxAmount - Amount exceeds maximum allowed for product/service
	AboveMaxAmount = "017"
	// LowWalletBalance - Insufficient funds in wallet
	LowWalletBalance = "018"
	// DuplicateTransaction - Multiple identical service requests within 30 seconds
	DuplicateTransaction = "019"
	// AccountLocked - Account is locked
	AccountLocked = "021"
	// AccountSuspended - Account is suspended
	AccountSuspended = "022"
	// APIAccessDisabled - API access not enabled for user
	APIAccessDisabled = "023"
	// AccountInactive - Account is inactive
	AccountInactive = "024"
	// InvalidBank - Invalid bank code for bank transfer
	InvalidBank = "025"
	// UnverifiedAccount - Bank account could not be verified
	UnverifiedAccount = "026"
	// IPNotWhitelisted - Server IP needs whitelisting
	IPNotWhitelisted = "027"
	// ProductNotWhitelisted - Product needs to be whitelisted for account
	ProductNotWhitelisted = "028"
	// BillerUnavailable - Biller for product/service is unreachable
	BillerUnavailable = "030"
	// BelowMinQuantity - Quantity below minimum allowed per transaction
	BelowMinQuantity = "031"
	// AboveMaxQuantity - Quantity exceeds maximum allowed per transaction
	AboveMaxQuantity = "032"
	// ServiceSuspended - Service temporarily suspended
	ServiceSuspended = "034"
	// ServiceInactive - Service currently turned off
	ServiceInactive = "035"
	// TransactionReversal - Transaction reversed to wallet
	TransactionReversal = "040"
	// SystemError - System error, contact tech support
	SystemError = "083"
	// ImproperRequestIDNoDate - Request ID missing valid date
	ImproperRequestIDNoDate = "085"
)
