package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	runtime "github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type handler struct {
	sesClient *ses.Client
	ssmClient *ssm.Client
}

type Message struct {
	CharSet   string `json:"charSet,omitempty"`
	HtmlBody  string `json:"htmlBody,omitempty"`
	Recipient string `json:"recipient,omitempty"`
	Sender    string `json:"sender,omitempty"`
	Subject   string `json:"subject,omitempty"`
	TextBody  string `json:"textBody,omitempty"`
}

type SendEmailAPI interface {
	SendEmail(ctx context.Context,
		params *ses.SendEmailInput,
		optFns ...func(*ses.Options)) (*ses.SendEmailOutput, error)
}

type SSMGetParametersAPI interface {
	GetParametersByPath(ctx context.Context,
		params *ssm.GetParametersByPathInput,
		optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
}

func SendEmail(ctx context.Context, api SendEmailAPI, input *ses.SendEmailInput) (*ses.SendEmailOutput, error) {
	return api.SendEmail(ctx, input)
}

func GetParametersByPath(ctx context.Context, api SSMGetParametersAPI, input *ssm.GetParametersByPathInput) (*ssm.GetParametersByPathOutput, error) {
	return api.GetParametersByPath(ctx, input)
}

func (h *handler) readParameters(path string) (*ssm.GetParametersByPathOutput, error) {
	input := &ssm.GetParametersByPathInput{
		Path:           aws.String(path),
		WithDecryption: true,
	}

	output, err := GetParametersByPath(context.TODO(), h.ssmClient, input)
	if err != nil {
		return nil, err
	}

	return output, nil
}

func (h *handler) sendNotification(ctx context.Context, msg Message) (*ses.SendEmailOutput, error) {
	input := &ses.SendEmailInput{
		Destination: &types.Destination{
			CcAddresses: []string{},
			ToAddresses: []string{
				msg.Recipient,
			},
		},
		Message: &types.Message{
			Body: &types.Body{
				Html: &types.Content{
					Charset: aws.String(msg.CharSet),
					Data:    aws.String(msg.HtmlBody),
				},
				Text: &types.Content{
					Charset: aws.String(msg.CharSet),
					Data:    aws.String(msg.TextBody),
				},
			},
			Subject: &types.Content{
				Charset: aws.String(msg.CharSet),
				Data:    aws.String(msg.Subject),
			},
		},
		Source: aws.String(msg.Sender),
	}

	output, err := SendEmail(context.TODO(), h.sesClient, input)
	if err != nil {
		return nil, err
	}

	fmt.Println("Email sent to " + msg.Recipient)
	return output, nil
}

func (h *handler) HandleRequest(ctx context.Context, msg Message) (string, error) {
	var sender string
	var recipient string

	output, err := h.readParameters("/shiftboard/notifications")
	if err != nil {
		return "", fmt.Errorf("error reading SSM parameter store: %v", err)
	}

	if len(output.Parameters) == 0 {
		return "", fmt.Errorf("no parameters returned from SSM parameter store")
	}

	for _, item := range output.Parameters {
		switch strings.Split(*item.Name, "/")[3] {
		case "sender":
			sender = *item.Value
		case "recipient":
			recipient = *item.Value
		}
	}

	msg.Sender = sender
	msg.Recipient = recipient
	msg.CharSet = "UTF-8"

	_, err = h.sendNotification(context.TODO(), msg)
	if err != nil {
		return "", fmt.Errorf("error sending SES notification: %v", err)
	}

	return fmt.Sprintf("Success"), nil
}

func main() {
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if os.Getenv("AWS_SAM_LOCAL") == "true" {
			return aws.Endpoint{
				PartitionID:   "aws",
				URL:           "http://host.docker.internal:4566",
				SigningRegion: os.Getenv("AWS_DEFAULT_REGION"),
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithEndpointResolverWithOptions(customResolver))
	if err != nil {
		fmt.Errorf("error loading default AWS configuration: %v", err)
		os.Exit(1)
	}

	h := handler{
		sesClient: ses.NewFromConfig(cfg),
		ssmClient: ssm.NewFromConfig(cfg),
	}

	runtime.Start(h.HandleRequest)
}
