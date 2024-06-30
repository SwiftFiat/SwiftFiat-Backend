package service

// assumes you have the following environment variables setup for AWS session creation
// AWS_SDK_LOAD_CONFIG=1
// AWS_ACCESS_KEY_ID=XXXXXXXXXX
// AWS_SECRET_ACCESS_KEY=XXXXXXXX
// AWS_REGION=us-west-2( or AWS_DEFAULT_REGION=us-east-1 if you are having trouble)

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

// Channel represents the type of notification channel
type Channel string

const (
	EMAIL Channel = "EMAIL"
	SMS   Channel = "SMS"
)

// OtpNotification represents a notification with channel options
type OtpNotification struct {
	Channel     Channel       `json:"channel"`
	PhoneNumber string        `json:"phone_number"`
	Email       string        `json:"email"`
	Config      *utils.Config `json:"config"`
}

func (o *OtpNotification) SendOTP() error {

	otpTemplate, err := getTemplate(OTPData{
		OTP: utils.GenerateOTP(),
	})
	if err != nil {
		return err
	}

	if o.Channel != SMS && o.Channel != EMAIL {
		err := fmt.Errorf("cannot decipher channel %v, please use either 'SMS' or 'EMAIL'", o.Channel)
		return err
	}

	if o.Channel == SMS && o.PhoneNumber == "" {
		err := fmt.Errorf("channel %v selected but no phone number provided", o.Channel)
		return err
	}

	if o.Channel == EMAIL && o.Email == "" {
		err := fmt.Errorf("channel %v selected but no email address provided", o.Channel)
		return err
	}

	if o.Channel == SMS {
		param := SmsNotification{
			Message:     otpTemplate.String(),
			PhoneNumber: o.PhoneNumber,
			Config:      o.Config,
		}

		return param.SendSMS()
	}

	if o.Channel == EMAIL {
		param := EmailNotification{
			Message: otpTemplate.String(),
			Email:   o.Email,
			Config:  o.Config,
		}

		return param.SendEmail()
	}

	return fmt.Errorf("unknown OTP Channel selected")

}

type OTPData struct {
	OTP string
}

func getTemplate(otp OTPData) (*strings.Builder, error) {
	// Parse the HTML template file
	tmpl, err := template.ParseFiles("templates/otp_template.html")
	if err != nil {
		return nil, fmt.Errorf("error parsing template: %v", err)
	}

	// Buffer to hold the rendered template
	var body strings.Builder

	// Execute the template with data and write the result to the buffer
	err = tmpl.Execute(&body, otp)
	if err != nil {
		return nil, fmt.Errorf("error executing template: %v", err)
	}

	return &body, nil
}
