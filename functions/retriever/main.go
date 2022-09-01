package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const paramPath = "/shiftboard/api"

type handler struct {
	workerFunction       string
	notificationFunction string
	ssmClient            *ssm.Client
	lambdaClient         *lambda.Client
}

type apiParameters struct {
	email       string
	password    string
	stateFilter string
}

type SSMGetParametersByPathAPI interface {
	GetParametersByPath(ctx context.Context,
		params *ssm.GetParametersByPathInput,
		optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
}

type LambdaInvokeAPI interface {
	Invoke(ctx context.Context,
		params *lambda.InvokeInput,
		optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)
}

func GetParametersByPath(ctx context.Context, api SSMGetParametersByPathAPI, path string, withDecryption bool) (*ssm.GetParametersByPathOutput, error) {
	return api.GetParametersByPath(ctx, &ssm.GetParametersByPathInput{
		Path:           aws.String(path),
		WithDecryption: withDecryption,
	})
}

func Invoke(ctx context.Context, api LambdaInvokeAPI, functionName string, payload []byte) (*lambda.InvokeOutput, error) {
	return api.Invoke(ctx, &lambda.InvokeInput{
		FunctionName:   aws.String(functionName),
		InvocationType: types.InvocationTypeEvent,
		Payload:        payload,
	})
}

func (h handler) HandleRequest(ctx context.Context) (string, error) {
	output, err := GetParametersByPath(context.TODO(), h.ssmClient, paramPath, true)
	if err != nil {
		return "", fmt.Errorf("error reading AWS parameter store: %v", err)
	}

	fmt.Printf("GetParametersByPath Output: %+v\n", output)

	config, err := parseParameters(output)
	if err != nil {
		return "", fmt.Errorf("error parsing parameters: %v", err)
	}

	apiClient, err := apiLogin(config.email, config.password)
	if err != nil {
		return "", fmt.Errorf("error with ShiftBoard API login: %v", err)
	}

	data, err := readFromAPI(apiClient)
	if err != nil {
		return "", fmt.Errorf("error retrieving data from ShiftBoard API: %v", err)
	}

	if config.stateFilter != "" {
		data = filterByState(data, config.stateFilter)
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("error marshalling ShiftBoard API data: %v", err)
	}

	fmt.Printf("Payload Size: %d\n", len(string(jsonData)))

	invokeOutput, err := Invoke(context.TODO(), h.lambdaClient, h.workerFunction, jsonData)
	if err != nil {
		return "", fmt.Errorf("error invoking function '%v': %v", h.workerFunction, err)
	}

	fmt.Printf("Lambda Output: %+v\n", invokeOutput)

	return "Success", nil
}

func filterByState(data *[]shiftboard.Shift, filter string) *[]shiftboard.Shift {
	var results []shiftboard.Shift
	for _, item := range *data {
		for _, state := range strings.Split(filter, ",") {
			if item.Location.State == state {
				results = append(results, item)
			}
		}
	}

	return &results
}

func parseParameters(output *ssm.GetParametersByPathOutput) (*apiParameters, error) {
	var params apiParameters
	if len(output.Parameters) == 0 {
		return &params, errors.New("no parameters returned from SSM parameter store")
	}

	for _, item := range output.Parameters {
		switch strings.Split(*item.Name, "/")[3] {
		case "email":
			params.email = *item.Value
		case "password":
			params.password = *item.Value
		case "state_filter":
			params.stateFilter = *item.Value
		}
	}

	return &params, nil
}

func apiLogin(email string, password string) (*shiftboard.Client, error) {
	// Validate email and password parameters
	if email == "" || password == "" {
		return nil, fmt.Errorf("API email or password parameters not found")
	}

	// Initialize ShiftBoard API client
	client := shiftboard.NewClient(email, password)

	// Retrieve list of sites for the API login
	resp, err := client.ListSites()
	if err != nil {
		return nil, fmt.Errorf("error calling ShiftBoard API ListSites (check credentials): %v", err)
	}

	// Extract Org ID from first site
	orgID := (*resp.Data.Sites)[0].OrgID

	// Set API access token on login
	_, err = client.Login(orgID)
	if err != nil {
		return nil, fmt.Errorf("error calling ShiftBoard API Login: %v", err)
	}

	return client, nil
}

func readFromAPI(client *shiftboard.Client) (*[]shiftboard.Shift, error) {
	// From now to 6 months
	currentTime := time.Now()
	startDate := currentTime.Format("2006-01-02")
	endDate := currentTime.AddDate(0, 6, 0).Format("2006-01-02")

	// Fetch list of shifts from API
	resp, err := client.ListShifts(startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("error calling ShiftBoard API ListShifts: %v", err)
	}

	return resp.Data.Shifts, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
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
		workerFunction:       getEnv("WORKER_FUNCTION", "WorkerFunction"),
		notificationFunction: getEnv("NOTIFICATION_FUNCTION", "NotificationFunction"),
		ssmClient:            ssm.NewFromConfig(cfg),
		lambdaClient:         lambda.NewFromConfig(cfg),
	}

	runtime.Start(h.HandleRequest)
}
