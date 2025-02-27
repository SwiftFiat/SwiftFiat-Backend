package notification_type

import (
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/notification/notification_channel"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

// OtpNotification represents a notification with channel options
// - implements NotificationType interface
type OtpNotification struct {
	Channel     notification_channel.Channel `json:"channel"`
	PhoneNumber string                       `json:"phone_number"`
	Email       string                       `json:"email"`
	Name        string                       `json:"name"`
	Expiry      string                       `json:"expiry"`
	Config      *utils.Config                `json:"config"`
}

func (o *OtpNotification) SendOTP(otp string) error {

	otpTemplate, err := getTemplate(OTPData{
		OTP:    otp,
		Name:   o.Name,
		Expiry: o.Expiry,
	}, "email-verification.html")
	if err != nil {
		return err
	}

	if o.Channel != notification_channel.SMS && o.Channel != notification_channel.EMAIL {
		err := fmt.Errorf("cannot decipher channel %v, please use either 'SMS' or 'EMAIL'", o.Channel)
		return err
	}

	if o.Channel == notification_channel.SMS && o.PhoneNumber == "" {
		err := fmt.Errorf("channel %v selected but no phone number provided", o.Channel)
		return err
	}

	if o.Channel == notification_channel.EMAIL && o.Email == "" {
		err := fmt.Errorf("channel %v selected but no email address provided", o.Channel)
		return err
	}

	if o.Channel == notification_channel.SMS {
		param := notification_channel.SmsNotification{
			Message:     otpTemplate.String(),
			PhoneNumber: o.PhoneNumber,
			Config:      o.Config,
		}

		return param.SendSMS()
	}

	if o.Channel == notification_channel.EMAIL {
		param := notification_channel.EmailNotification{
			Message: otpTemplate.String(),
			Email:   o.Email,
			Config:  o.Config,
			Subject: "SwiftFiat - OTP Verification",
		}

		return param.SendEmail()
	}

	return fmt.Errorf("unknown OTP Channel selected")

}

type OTPData struct {
	OTP    string
	Name   string
	Expiry string
}
