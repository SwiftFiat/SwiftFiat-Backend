package utils

import (
	"fmt"
	"math/rand"
	"time"
)

// Function to generate 4-digit OTP
func GenerateOTP() string {
	rand.NewSource(time.Now().UnixNano()) // Seed the random number generator with current time

	otp := rand.Intn(10000)         // Generate a random number between 0 to 9999
	return fmt.Sprintf("%04d", otp) // Format the OTP to always be 4 digits
}
