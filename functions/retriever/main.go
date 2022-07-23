package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	runtime "github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/edevenport/shiftboard-sdk-go"
)

type handler struct {
	ssmClient    *ssm.Client
	lambdaClient *lambda.Client
}

type SSMGetParametersAPI interface {
	GetParametersByPath(ctx context.Context,
		params *ssm.GetParametersByPathInput,
		optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
}

type LambdaInvokeAPI interface {
	Invoke(ctx context.Context,
		params *lambda.InvokeInput,
		optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)
}

func GetParametersByPath(ctx context.Context, api SSMGetParametersAPI, input *ssm.GetParametersByPathInput) (*ssm.GetParametersByPathOutput, error) {
	return api.GetParametersByPath(ctx, input)
}

func Invoke(ctx context.Context, api LambdaInvokeAPI, input *lambda.InvokeInput) (*lambda.InvokeOutput, error) {
	return api.Invoke(ctx, input)
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

func apiLogin(email string, password string) (*shiftboard.Client, error) {
	// Initialize ShiftBoard API client
	client := shiftboard.NewClient(email, password)

	// Retrieve list of sites for the API login
	resp, err := client.ListSites()
	if err != nil {
		log.Fatal(err)
	}

	// Extract Org ID from first site
	orgID := (*resp.Data.Sites)[0].OrgID

	// Set API access token on login
	_, err = client.Login(orgID)
	if err != nil {
		log.Fatal(err)
	}

	return client, nil
}

func readFromAPI(client *shiftboard.Client) (*[]shiftboard.Shift, error) {
	currentTime := time.Now()
	startDate := currentTime.AddDate(0, -1, 0).Format(time.RFC3339)
	endDate := currentTime.AddDate(0, 6, 0).Format(time.RFC3339)

	// Fetch list of shifts from API
	resp, err := client.ListShifts(startDate, endDate)
	if err != nil {
		log.Fatal(err)
	}

	return resp.Data.Shifts, nil
}

func (h *handler) invokeFunction(functionName string, payload []byte) (*lambda.InvokeOutput, error) {
	input := &lambda.InvokeInput{
		FunctionName:   aws.String(functionName),
		InvocationType: types.InvocationTypeEvent,
		Payload:        payload,
	}

	output, err := Invoke(context.TODO(), h.lambdaClient, input)
	if err != nil {
		return nil, err
	}

	return output, nil
}

func (h handler) HandleRequest(ctx context.Context) (string, error) {
	var email string
	var password string

	output, err := h.readParameters("/shiftboard/api")
	if err != nil {
		return "", fmt.Errorf("error reading AWS parameter store: %v", err)
	}

	for _, item := range output.Parameters {
		switch strings.Split(*item.Name, "/")[3] {
		case "email":
			email = *item.Value
		case "password":
			password = *item.Value
		}
	}

	apiClient, err := apiLogin(email, password)
	if err != nil {
		return "", fmt.Errorf("error logging into the ShiftBoard API: %v", err)
	}

	data, err := readFromAPI(apiClient)
	if err != nil {
		return "", fmt.Errorf("error retrieving data from ShiftBoard API: %v", err)
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("error marshalling data: %v", err)
	}

	functionName := os.Getenv("WRITER_FUNCTION")
	_, err = h.invokeFunction(functionName, jsonData)
	if err != nil {
		return "", fmt.Errorf("error invoking child function: %v", err)
	}

	return fmt.Sprintf("Success"), nil
}

func main() {
	// fmt.Println(os.Getenv("WRITER_FUNCTION"))
	// fmt.Println(os.Getenv("NOTIFICATION_FUNCTION"))

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
		fmt.Printf("error loading default AWS configuration: %v\n", err)
		os.Exit(1)
	}

	h := handler{
		ssmClient:    ssm.NewFromConfig(cfg),
		lambdaClient: lambda.NewFromConfig(cfg),
	}

	runtime.Start(h.HandleRequest)
}
