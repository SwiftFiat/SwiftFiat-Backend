package utils

import (
	"fmt"
	"log"

	"github.com/spf13/viper"
)

var (
	EnvPath string = "."
)

type Config struct {
	Env                  string `mapstructure:"ENV"`
	ServerPort           int    `mapstructure:"SERVER_PORT"`
	SigningKey           string `mapstructure:"SIGNING_KEY"`
	ServerBaseURL        string `mapstructure:"SERVER_BASE_URL"`
	AWSRegion            string `mapstructure:"AWS_REGION"`
	AWSAccessKeyID       string `mapstructure:"AWS_ACCESS_KEY"`
	AWSSecretAccessKey   string `mapstructure:"AWS_SECRET_ACCESS_KEY"`
	DBUsername           string `mapstructure:"DB_USERNAME"`
	DBPassword           string `mapstructure:"DB_PASSWORD"`
	DBHost               string `mapstructure:"DB_HOST"`
	DBPort               string `mapstructure:"DB_PORT"`
	DBDriver             string `mapstructure:"DB_DRIVER"`
	DBName               string `mapstructure:"DB_NAME"`
	SSLMode              string `mapstructure:"SSLMODE"`
	OTPSourceMail        string `mapstructure:"OTP_SOURCE_MAIL"`
	Papertrail           string `mapstructure:"PAPERTRAIL"`
	PapertrailAppName    string `mapstructure:"PAPERTRAIL_APP_NAME"`
	RedisHost            string `mapstructure:"REDIS_HOST"`
	RedisPort            string `mapstructure:"REDIS_PORT"`
	RedisPassword        string `mapstructure:"REDIS_PASSWORD"`
	Phone                string `mapstructure:"PHONE"`
	CountryCode          string `mapstructure:"COUNTRYCODE"`
	Email                string `mapstructure:"EMAIL"`
	CryptomusBaseUrl     string `mapstructure:"CRYPTOMUS_BASE_URL"`
	CryptomusApiKey      string `mapstructure:"CRYPTOMUS_API_KEY"`
	CryptomusMerchantId  string `mapstructure:"CRYPTOMUS_MERCHANT_ID"`
	CryptomusCallbackUrl string `mapstructure:"CRYPTOMUS_CALLBACK_URL"`
}

func LoadConfig(path string) (*Config, error) {
	// Validate that the path is not empty
	if path == "" {
		path = "."
	}

	// Create a new Viper instance to avoid global state
	v := viper.New()

	// Disable environment variable prefix
	v.SetEnvPrefix("")
	v.AutomaticEnv()

	// Configure config file
	v.AddConfigPath(path)
	v.SetConfigName(".env")
	v.SetConfigType("env")

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		// Log the error, but don't fail entirely
		log.Printf("Warning: Unable to read config file: %v", err)
	}

	// Bind environment variables explicitly
    _ = v.BindEnv("SERVER_PORT")
    _ = v.BindEnv("SIGNING_KEY")
    _ = v.BindEnv("TOKEN_TTL")
    _ = v.BindEnv("DB_USERNAME")
    _ = v.BindEnv("DB_PASSWORD")
    _ = v.BindEnv("DB_HOST")
    _ = v.BindEnv("DB_NAME")
    _ = v.BindEnv("DB_PORT")
    _ = v.BindEnv("SSLMODE")
    _ = v.BindEnv("DB_DRIVER")
    _ = v.BindEnv("PAPERTRAIL")
    _ = v.BindEnv("KYC_PROVIDER_NAME")
	_ = v.BindEnv("DOJAH_KEY")
	_ = v.BindEnv("DOJAH_APP_ID")
	_ = v.BindEnv("DOJAH_BASE_URL")
	_ = v.BindEnv("GIFTCARD_PROVIDER_NAME")
	_ = v.BindEnv("GIFTCARD_KEY")
	_ = v.BindEnv("GIFTCARD_APP_ID")
	_ = v.BindEnv("GIFTCARD_BASE_URL")
	_ = v.BindEnv("GIFTCARD_AUTH_URL")
	_ = v.BindEnv("GIFTCARD_PROD_KEY")
	_ = v.BindEnv("GIFTCARD_PROD_ID")
	_ = v.BindEnv("CRYPTO_PROVIDER_NAME")
	_ = v.BindEnv("BITGO_PORT")
	_ = v.BindEnv("BITGO_HOST")
	_ = v.BindEnv("BITGO_ACCESS_KEY")
	_ = v.BindEnv("BITGO_ENTERPRISE")
	_ = v.BindEnv("BITGO_WALLET_PASSKEY")
	_ = v.BindEnv("COIN_DATA_PROVIDER_NAME")
	_ = v.BindEnv("COINRANKING_BASE_URL")
	_ = v.BindEnv("COINRANKING_ACCESS_KEY")
	_ = v.BindEnv("REDIS_HOST")
	_ = v.BindEnv("REDIS_PORT")
	_ = v.BindEnv("REDIS_PASSWORD")
	_ = v.BindEnv("GOOGLE_APPLICATION_CREDENTIALS")
	_ = v.BindEnv("FIAT_PROVIDER_NAME")
	_ = v.BindEnv("PAYSTACK_KEY")
	_ = v.BindEnv("PAYSTACK_BASE_URL")
	_ = v.BindEnv("BILL_PROVIDER_NAME")
	_ = v.BindEnv("VT_BASE_URL")
	_ = v.BindEnv("VT_PASS_KEY")
	_ = v.BindEnv("VT_PASS_PK")
	_ = v.BindEnv("VT_PASS_SK")
	_ = v.BindEnv("CRYPTOMUS_PROVIDER_NAME")
	_ = v.BindEnv("CRYPTOMUS_BASE_URL")
	_ = v.BindEnv("CRYPTOMUS_MERCHANT_ID")
	_ = v.BindEnv("CRYPTOMUS_API_KEY")
	_ = v.BindEnv("CRYPTOMUS_CALLBACK_URL")
	


	// Create config struct
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("unable to decode config: %w", err)
	}

	// Additional security: Validate critical configurations
	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

func validateConfig(config *Config) error {
	// Add validation for critical configurations
	if config.ServerPort == 0 {
		return fmt.Errorf("server port must be specified")
	}

	// Add more validation as needed
	if config.DBUsername == "" || config.DBPassword == "" {
		return fmt.Errorf("database credentials must be provided")
	}

	return nil
}

// Optional: Masking sensitive information for logging
func (c *Config) Redact() Config {
	redacted := *c
	redacted.AWSSecretAccessKey = "****"
	redacted.DBPassword = "****"
	redacted.RedisPassword = "****"
	return redacted
}

func LoadCustomConfig(path string, val interface{}) error {
	// Validate that the path is not empty
	if path == "" {
		path = "."
	}

	// Create a new Viper instance to avoid global state
	v := viper.New()

	// Allow overriding config via environment variables
	v.SetEnvPrefix("SWIFT") // Prefix for env vars
	v.AutomaticEnv()

	// Configure config file
	v.AddConfigPath(path)
	v.SetConfigName(".env")
	v.SetConfigType("env")

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		// Log the error, but don't fail entirely
		log.Printf("Warning: Unable to read config file: %v", err)
	}

	if err := v.Unmarshal(&val); err != nil {
		return fmt.Errorf("unable to decode config: %w", err)
	}

	// Additional security: Validate critical configurations
	// TODO: Enable critical validation later
	return nil
}
