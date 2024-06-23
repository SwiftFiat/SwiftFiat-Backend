package service

// assumes you have the following environment variables setup for AWS session creation
// AWS_SDK_LOAD_CONFIG=1
// AWS_ACCESS_KEY_ID=XXXXXXXXXX
// AWS_SECRET_ACCESS_KEY=XXXXXXXX
// AWS_REGION=us-west-2( or AWS_DEFAULT_REGION=us-east-1 if you are having trouble)

import (
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/pinpoint"
)

type OtpNotification struct {
	Channel     string        `json:"channel"`
	PhoneNumber string        `json:"phone_number"`
	Email       string        `json:"email"`
	Config      *utils.Config `json:"config"`
}

func (o *OtpNotification) SendOTP() error {

	if o.Channel != "SMS" && o.Channel != "EMAIL" {
		err := fmt.Errorf("cannot decipher channel %v, please use either 'SMS' or 'EMAIL'", o.Channel)
		return err
	}

	if o.Channel == "SMS" && o.PhoneNumber == "" {
		err := fmt.Errorf("channel %v selected but no phone number provided", o.Channel)
		return err
	}

	if o.Channel == "EMAIL" && o.Email == "" {
		err := fmt.Errorf("channel %v selected but no email address provided", o.Channel)
		return err
	}

	AwsRegion := o.Config.AWSRegion
	AccessKeyID := o.Config.AWSAccessKeyID
	SecretAccessKey := o.Config.AWSSecretAccessKey

	// Create Session and assign AccessKeyID and SecretAccessKey
	sess := session.Must(session.NewSession(
		&aws.Config{
			Region:      aws.String(AwsRegion),
			Credentials: credentials.NewStaticCredentials(AccessKeyID, SecretAccessKey, ""),
		},
	))

	// Create a Pinpoint client from just a session.
	svc := pinpoint.New(sess)
	ref := "123"

	// if ref == "123" {
	// 	panic(fmt.Errorf("ref is 123, please change it"))
	// }

	var destination string

	if o.Channel != "SMS" {
		destination = o.PhoneNumber
	} else {
		destination = o.Email
	}

	params := &pinpoint.SendOTPMessageInput{
		ApplicationId:                   &o.Config.PinPointAppID,
		SendOTPMessageRequestParameters: &pinpoint.SendOTPMessageRequestParameters{BrandName: aws.String(o.Config.BrandName), Channel: aws.String(o.Channel), DestinationIdentity: aws.String(destination), OriginationIdentity: aws.String("TEST"), ReferenceId: aws.String(ref), AllowedAttempts: aws.Int64(3), CodeLength: aws.Int64(4)},
	}

	resp, err := svc.SendOTPMessage(params)

	if err != nil {
		// Print the error, cast err to awserr.Error to get the Code and
		// Message from an error.
		if awsError, ok := err.(awserr.Error); ok {
			return fmt.Errorf("awsError: %v, awsErrorCode: %v, awsErrorMessage: %v", awsError, awsError.Code(), awsError.Message())
		}
		fmt.Println(err.Error())
		return err
	}

	// Pretty-print the response data.
	fmt.Println(resp)

	return nil
}
