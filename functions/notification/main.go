package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/edevenport/shiftboard-sdk-go"

	runtime "github.com/aws/aws-lambda-go/lambda"
)

const (
	charSet   = "UTF-8"
	paramPath = "/shiftboard/notifications"
)

type handler struct {
	sesClient *ses.Client
	ssmClient *ssm.Client
}

type Diff struct {
	State string
	Shift shiftboard.Shift
}

type Message struct {
	HtmlBody string `json:"htmlBody,omitempty"`
	Subject  string `json:"subject,omitempty"`
	TextBody string `json:"textBody,omitempty"`
}

type SESSendEmailAPI interface {
	SendEmail(ctx context.Context,
		params *ses.SendEmailInput,
		optFns ...func(*ses.Options)) (*ses.SendEmailOutput, error)
}

type SSMGetParametersByPathAPI interface {
	GetParametersByPath(ctx context.Context,
		params *ssm.GetParametersByPathInput,
		optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
}

func SendEmail(ctx context.Context, api SESSendEmailAPI, sender string, recipient string, msg Message) (*ses.SendEmailOutput, error) {
	return api.SendEmail(ctx, &ses.SendEmailInput{
		Destination: &types.Destination{
			CcAddresses: []string{},
			ToAddresses: []string{
				recipient,
			},
		},
		Message: &types.Message{
			Body: &types.Body{
				Html: &types.Content{
					Charset: aws.String(charSet),
					Data:    aws.String(msg.HtmlBody),
				},
				Text: &types.Content{
					Charset: aws.String(charSet),
					Data:    aws.String(msg.TextBody),
				},
			},
			Subject: &types.Content{
				Charset: aws.String(charSet),
				Data:    aws.String(msg.Subject),
			},
		},
		Source: aws.String(sender),
	})
}

func GetParametersByPath(ctx context.Context, api SSMGetParametersByPathAPI, path string, withDecryption bool) (*ssm.GetParametersByPathOutput, error) {
	return api.GetParametersByPath(ctx, &ssm.GetParametersByPathInput{
		Path:           aws.String(path),
		WithDecryption: withDecryption,
	})
}

func (h *handler) HandleRequest(ctx context.Context, payload Diff) (string, error) {
	// Read notification parameters from SSM Parameter Store
	params, err := GetParametersByPath(context.TODO(), h.ssmClient, paramPath, false)
	if err != nil {
		return "", fmt.Errorf("error reading from SSM parameter store: %v", err)
	}

	// Extract sender and recipient from parameters
	sender, recipient, err := parseParameters(params)
	if err != nil {
		return "", fmt.Errorf("error parsing parameters: %v", err)
	}

	// Construct email template
	msg := constructMessage(&payload)

	// Send email to recipients
	output, err := SendEmail(context.TODO(), h.sesClient, sender, recipient, msg)
	if err != nil {
		return "", fmt.Errorf("error sending SES notification: %v", err)
	}

	fmt.Println("Message ID:", *output.MessageId)
	fmt.Println("Email sent to " + recipient)

	return "Success", nil
}

func parseParameters(output *ssm.GetParametersByPathOutput) (sender string, recipient string, err error) {
	if len(output.Parameters) == 0 {
		return "", "", errors.New("no parameters returned from SSM parameter store")
	}

	for _, item := range output.Parameters {
		switch strings.Split(*item.Name, "/")[3] {
		case "sender":
			sender = *item.Value
		case "recipient":
			recipient = *item.Value
		}
	}

	return sender, recipient, nil
}

func constructMessage(item *Diff) (msg Message) {
	shift := item.Shift

	tmplDate := formatDate(item)
	tmpl := generateTemplate(item.State)

	msg.Subject = fmt.Sprintf(tmpl.Subject, shift.Name)
	msg.TextBody = fmt.Sprintf(tmpl.TextBody, shift.Name, tmplDate, shift.ID)
	msg.HtmlBody = fmt.Sprintf(tmpl.HtmlBody, shift.ID, shift.Name, tmplDate)

	return msg
}

func formatDate(item *Diff) string {
	startDate, _ := time.Parse(time.RFC3339, item.Shift.StartDate+"Z")
	dateTime := map[string]string{
		"created": startDate.Format(time.RFC1123),
		"updated": item.Shift.Updated.Format(time.RFC1123),
	}

	return dateTime[item.State]
}

func main() {
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if os.Getenv("AWS_SAM_LOCAL") == "true" {
			return aws.Endpoint{
				PartitionID:   "aws",
				URL:           "http://host.docker.internal:4566",
				SigningRegion: os.Getenv("AWS_REGION"),
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithEndpointResolverWithOptions(customResolver))
	if err != nil {
		fmt.Printf("error loading default AWS configuration: %v\n", err)
		os.Exit(1)
	}

	h := handler{
		sesClient: ses.NewFromConfig(cfg),
		ssmClient: ssm.NewFromConfig(cfg),
	}

	runtime.Start(h.HandleRequest)
}
