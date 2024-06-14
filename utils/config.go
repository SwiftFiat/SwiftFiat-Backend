package utils

import "github.com/spf13/viper"

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
