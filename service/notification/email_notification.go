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
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ses"
)

type EmailNotification struct {
	Message string        `json:"message"`
	Email   string        `json:"email"`
	Subject string        `json:"subject"`
	Config  *utils.Config `json:"config"`
}

func (e *EmailNotification) SendEmail() error {

	AwsRegion := e.Config.AWSRegion
	AccessKeyID := e.Config.AWSAccessKeyID
	SecretAccessKey := e.Config.AWSSecretAccessKey

	// Create Session and assign AccessKeyID and SecretAccessKey
	sess := session.Must(session.NewSession(
		&aws.Config{
			Region:      aws.String(AwsRegion),
			Credentials: credentials.NewStaticCredentials(AccessKeyID, SecretAccessKey, ""),
		},
	))

	// Create a new instance of the AWS SES service
	svc := ses.New(sess)

	// Set the sender and recipient email addresses
	from := e.Config.OTPSourceMail
	to := e.Email

	// Set the email subject and body
	subject := "OTP"
	body := e.Message

	// Send the email
	result, err := svc.SendEmail(&ses.SendEmailInput{
		Destination: &ses.Destination{
			ToAddresses: []*string{
				aws.String(to),
			},
		},
		Message: &ses.Message{
			Body: &ses.Body{
				Html: &ses.Content{
					Charset: aws.String("UTF-8"),
					Data:    aws.String(body),
				},
			},
			Subject: &ses.Content{
				Charset: aws.String("UTF-8"),
				Data:    aws.String(subject),
			},
		},
		Source: aws.String(from),
	})
	if err != nil {
		return err
	}

	// Print the response from AWS SES
	fmt.Println(result)

	return nil
}
