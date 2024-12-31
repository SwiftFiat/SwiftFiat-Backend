package utils

import (
	"fmt"
	"log"
	"math/rand"
	"time"
)

type OTPObject struct {
	OTP    string
	Expiry time.Time
}

// Function to generate 4-digit OTP
func GenerateOTP() string {
	rand.NewSource(time.Now().UnixNano()) // Seed the random number generator with current time

	otp := rand.Intn(10000)         // Generate a random number between 0 to 9999
	return fmt.Sprintf("%04d", otp) // Format the OTP to always be 4 digits
}

// Returns [true] if OTP is correct and valid, else return [false]
func CompareOTP(sourceOTP string, storedOTP OTPObject) bool {
	if sourceOTP != storedOTP.OTP {
		log.Default().Output(6, "OTP Mismatch")
		return false
	}

	if storedOTP.Expiry.Before(time.Now()) {
		log.Default().Output(6, "OTP Expired")
		return false
	}

	return true
}
