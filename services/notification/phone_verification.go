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

func (t *Twilio) SendVerificationCode(phone string) error {
	var client = twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: t.Config.TwilioKeySid,
		Password: t.Config.TwilioKeySecret,
		AccountSid: t.Config.TWILIO_ACCOUNT_SID,
	})

	log := logging.NewLogger()
	log.Info("twilio username: ", t.Config.TwilioKeySid)
	log.Info("twilio password: ", t.Config.TwilioKeySecret)
	log.Info("twilio verify service sid: ", t.Config.TWILIO_VERIFY_SERVICE_SID)
	log.Info("req phone: ", phone)

	if t.Config.TWILIO_VERIFY_SERVICE_SID == "" {
		log.Error("Twilio Verify Service SID is not set")
		return errors.New("twilio Verify Service SID is not configured")
	}

	// channels := []string{"sms", "whatsapp", "call"}
	params := &verify.CreateVerificationParams{}
	params.SetTo(phone)
	// for _, channel := range channels {
	params.SetChannel("whatsapp")
	// }
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
		Username: t.Config.TwilioKeySid,
		Password: t.Config.TwilioKeySecret,
		AccountSid: t.Config.TWILIO_ACCOUNT_SID,
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
