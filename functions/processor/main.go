package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	runtime "github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/edevenport/shiftboard-sdk-go"
)

type handler struct {
	tableName    string
	dbClient     *dynamodb.Client
	lambdaClient *lambda.Client
}

type Message struct {
	CharSet   string `json:"charSet,omitempty"`
	HtmlBody  string `json:"htmlBody,omitempty"`
	Recipient string `json:"recipient,omitempty"`
	Sender    string `json:"sender,omitempty"`
	Subject   string `json:"subject,omitempty"`
	TextBody  string `json:"textBody,omitempty"`
}

type DynamoDBPutItemAPI interface {
	PutItem(ctx context.Context,
		params *dynamodb.PutItemInput,
		optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
}

type DynamoDBScanAPI interface {
	Scan(ctx context.Context,
		params *dynamodb.ScanInput,
		optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
}

type LambdaInvokeAPI interface {
	Invoke(ctx context.Context,
		params *lambda.InvokeInput,
		optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)
}

func (h *handler) PutItem(ctx context.Context, api DynamoDBPutItemAPI, tableName string, item map[string]dbtypes.AttributeValue) (*dynamodb.PutItemOutput, error) {
	return api.PutItem(ctx, &dynamodb.PutItemInput{
		Item:      item,
		TableName: aws.String(tableName),
	})
}

func (h *handler) Scan(ctx context.Context, api DynamoDBScanAPI, tableName string) (*dynamodb.ScanOutput, error) {
	return api.Scan(ctx, &dynamodb.ScanInput{
		TableName: aws.String(tableName),
		Limit:     aws.Int32(500),
	})
}

func (h *handler) Invoke(ctx context.Context, api LambdaInvokeAPI, functionName string, payload []byte) (*lambda.InvokeOutput, error) {
	return api.Invoke(ctx, &lambda.InvokeInput{
		FunctionName:   aws.String(functionName),
		InvocationType: lambdatypes.InvocationTypeEvent,
		Payload:        payload,
	})
}

func (h *handler) compareData(newData *[]shiftboard.Shift, cachedData *[]shiftboard.Shift) {
	for i := 0; i < len(*newData); i++ {
		shift := (*newData)[i]
		exists, updated := getState(shift, cachedData)

		if !exists {
			fmt.Printf("Shift has been added: %s on %s\n", shift.Name, shift.Created)
			h.sendNotification("New shift added", shift.Name)
		}
		if updated {
			fmt.Printf("Shift has been updated: %s\n", shift.Name)
			h.sendNotification("Shift has been updated", shift.Name)
		}
	}
}

func (h *handler) writeToDB(tableName string, payload []shiftboard.Shift) error {
	for _, item := range payload {
		av, err := attributevalue.MarshalMap(item)
		if err != nil {
			return fmt.Errorf("error marshalling DynamoDB attribute value map: %v", err)
		}

		_, err = h.PutItem(context.TODO(), h.dbClient, tableName, av)
		if err != nil {
			return fmt.Errorf("error calling DynamoDB PutItem: %v", err)
		}

		fmt.Println("Successfully added '" + item.Name + "' to table " + tableName)
	}

	return nil
}

func (h *handler) sendNotification(subject string, body string) error {
	msg := Message{
		Subject:  subject,
		TextBody: body,
		HtmlBody: fmt.Sprintf("<p>%s</p>", body),
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("error marshalling message payload: %v", err)
	}

	_, err = h.Invoke(context.TODO(), h.lambdaClient, "functionName", payload)
	if err != nil {
		return fmt.Errorf("error calling Lambda Invoke: %v", err)
	}

	return nil
}

func getState(shift shiftboard.Shift, cache *[]shiftboard.Shift) (exists bool, updated bool) {
	exists = false
	updated = false

	for _, c := range *cache {
		if c.ID == shift.ID {
			exists = true
			if c.Updated.Before(shift.Updated) {
				updated = true
			}
			break
		}
	}

	return exists, updated
}

func unmarshalDBItems(cachedData *dynamodb.ScanOutput) ([]shiftboard.Shift, error) {
	items := []shiftboard.Shift{}

	err := attributevalue.UnmarshalListOfMaps(cachedData.Items, &items)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling DynamoDB list of maps: %v", err)
	}

	return items, nil
}

func (h *handler) HandleRequest(ctx context.Context, payload []shiftboard.Shift) (string, error) {
	// End function runtime if payload is empty
	if len(payload) == 0 {
		return "", fmt.Errorf("function payload is empty")
	}

	// fmt.Printf("Payload: %+v\n\n", payload)

	// Read existing items from DynamoDB table
	dbOutput, err := h.Scan(context.TODO(), h.dbClient, h.tableName)
	if err != nil {
		return "", fmt.Errorf("error scanning DynamoDB table: %v", err)
	}

	cachedData, err := unmarshalDBItems(dbOutput)

	// Write data to DynamoDB table and end runtime if no data exists
	if dbOutput.Count == 0 {
		h.writeToDB(h.tableName, payload)
		return "Success", nil
	}

	// Compare results
	h.compareData(&payload, &cachedData)

	// Write new data to DynamoDB table
	h.writeToDB(h.tableName, payload)

	// return fmt.Sprintf("Success"), nil
	return "Success", nil
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
		fmt.Printf("error loading default AWS configuration: %v\n", err)
		os.Exit(1)
	}

	h := handler{
		tableName:    os.Getenv("TABLE_NAME"),
		dbClient:     dynamodb.NewFromConfig(cfg),
		lambdaClient: lambda.NewFromConfig(cfg),
	}

	runtime.Start(h.HandleRequest)
}
