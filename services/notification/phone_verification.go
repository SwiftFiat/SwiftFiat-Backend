package service

import (
	"errors"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/twilio/twilio-go"
	verify "github.com/twilio/twilio-go/rest/verify/v2"
)

type Twilio struct {
	Config *utils.Config
}

// var client = twilio.NewRestClientWithParams(twilio.ClientParams{
// 	Username: config.TWILIO_ACCOUNT_SID,
// 	Password: config.TWILIO_AUTH_TOKEN,
// })

func (t *Twilio) SendVerificationCode(phone string) error {
	var client = twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: t.Config.TWILIO_ACCOUNT_SID,
		Password: t.Config.TWILIO_AUTH_TOKEN,
	})

	log := logging.NewLogger()
	log.Info("twilio username: ", t.Config.TWILIO_ACCOUNT_SID)
	log.Info("twilio password: ", t.Config.TWILIO_AUTH_TOKEN)
	log.Info("twilio verify service sid: ", t.Config.TWILIO_VERIFY_SERVICE_SID)
	log.Info("req phone: ", phone)

	if t.Config.TWILIO_VERIFY_SERVICE_SID == "" {
		log.Error("Twilio Verify Service SID is not set")
		return errors.New("twilio Verify Service SID is not configured")
	}

	channels := []string{"sms", "whatsapp", "call"}
	params := &verify.CreateVerificationParams{}
	params.SetTo(phone)
	for _, channel := range channels {
		params.SetChannel(channel)
	}
	params.SetCustomFriendlyName("SwiftFiat")
	// params.SetChannel("sms")

	_, err := client.VerifyV2.CreateVerification(t.Config.TWILIO_VERIFY_SERVICE_SID, params)
	if err != nil {
		log.Error("Twilio verification error: ", err.Error())
		return err
	}

	return nil
}

func (t *Twilio) CheckVerificationCode(phone string, code string) (bool, error) {
	var client = twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: t.Config.TWILIO_ACCOUNT_SID,
		Password: t.Config.TWILIO_AUTH_TOKEN,
	})
	params := &verify.CreateVerificationCheckParams{}
	params.SetTo(phone)
	params.SetCode(code)

	resp, err := client.VerifyV2.CreateVerificationCheck(t.Config.TWILIO_VERIFY_SERVICE_SID, params)
	if err != nil {
		return false, err
	}

	return *resp.Status == "approved", nil
}
