package utils

import "github.com/spf13/viper"

var (
	EnvPath string = "."
)

type Config struct {
	ServerPort         int    `mapstructure:"SERVER_PORT"`
	SigningKey         string `mapstructure:"SIGNING_KEY"`
	AWSRegion          string `mapstructure:"AWS_REGION"`
	AWSAccessKeyID     string `mapstructure:"AWS_ACCESS_KEY"`
	AWSSecretAccessKey string `mapstructure:"AWS_SECRET_ACCESS_KEY"`
	DBUsername         string `mapstructure:"DB_USERNAME"`
	DBPassword         string `mapstructure:"DB_PASSWORD"`
	DBHost             string `mapstructure:"DB_HOST"`
	DBPort             string `mapstructure:"DB_PORT"`
	DBDriver           string `mapstructure:"DB_DRIVER"`
	DBName             string `mapstructure:"DB_NAME"`
	OTPSourceMail      string `mapstructure:"OTP_SOURCE_MAIL"`
	Papertrail         string `mapstructure:"PAPERTRAIL"`
	PapertrailAppName  string `mapstructure:"PAPERTRAIL_APP_NAME"`
	RedisHost          string `mapstructure:"REDIS_HOST"`
	RedisPort          string `mapstructure:"REDIS_PORT"`
	RedisPassword      string `mapstructure:"REDIS_PASSWORD"`
}

func LoadConfig(path string) (config *Config, err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName(".env")
	viper.SetConfigType("env")

	viper.AutomaticEnv()

	err = viper.ReadInConfig()
	if err != nil {
		return nil, err
	}

	err = viper.Unmarshal(&config)
	if err != nil {
		return nil, err
	}

	return config, nil
}

func LoadCustomConfig(path string, val interface{}) (err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName(".env")
	viper.SetConfigType("env")

	viper.AutomaticEnv()

	err = viper.ReadInConfig()
	if err != nil {
		return err
	}

	err = viper.Unmarshal(val)
	if err != nil {
		return err
	}

	return nil
}
