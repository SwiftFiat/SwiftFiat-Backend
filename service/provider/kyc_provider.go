package provider

// KYCProvider specific to KYC operations
type KYCProvider interface {
	BaseProvider
	VerifyBVN() interface{}
	VerifyNIN() string
}
