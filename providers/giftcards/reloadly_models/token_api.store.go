package reloadlymodels

type Audience string

var (
	PROD    Audience = "prod"
	SANDBOX Audience = "sandbox"
)

type TokenApiStore struct {
	Audience Audience
	Token    TokenApiResponse
}
