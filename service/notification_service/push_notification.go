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
	"github.com/aws/aws-sdk-go/service/sns"
)

type PushNotification struct {
	Message      string        `json:"message"`
	UserFCMToken string        `json:"user_fcm_token"`
	Config       *utils.Config `json:"config"`
}

func (p *PushNotification) SendPush() error {
	AwsRegion := p.Config.AWSRegion
	AccessKeyID := p.Config.AWSAccessKeyID
	SecretAccessKey := p.Config.AWSSecretAccessKey

	// Create Session and assign AccessKeyID and SecretAccessKey
	sess := session.Must(session.NewSession(
		&aws.Config{
			Region:      aws.String(AwsRegion),
			Credentials: credentials.NewStaticCredentials(AccessKeyID, SecretAccessKey, ""),
		},
	))

	// Create SNS service
	svc := sns.New(sess)

	params := &sns.PublishInput{
		Message:     aws.String(p.Message),
		PhoneNumber: aws.String(p.UserFCMToken),
	}
	resp, err := svc.Publish(params)

	if err != nil {
		// Print the error, cast err to awserr.Error to get the Code and
		// Message from an error.
		fmt.Println(err.Error())
		panic(err)
		return err
	}

	// Pretty-print the response data.
	fmt.Println(resp)

	return nil
}
