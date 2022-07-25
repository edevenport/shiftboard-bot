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
	HTMLBody  string `json:"htmlBody,omitempty"`
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

func (h *handler) compareData(newData *[]shiftboard.Shift, cachedData *[]shiftboard.Shift) error {
	for i := 0; i < len(*newData); i++ {
		shift := (*newData)[i]
		exists, updated := getState(shift, cachedData)

		var subject string
		var msgBody string

		if !exists {
			subject = fmt.Sprintf("New shift added: %s", shift.Name)
			msgBody = fmt.Sprintf("Shift has been added for '%s' on %s", shift.Name, shift.Created)
		}

		if updated {
			subject = fmt.Sprintf("Shift updated: %s", shift.Name)
			msgBody = fmt.Sprintf("Shift for '%s' was updated on %s", shift.Name, shift.Updated)
		}

		if err := h.sendNotification(subject, msgBody); err != nil {
			return fmt.Errorf("Error sending notification: %v", err)
		}
	}

	return nil
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
		HTMLBody: fmt.Sprintf("<p>%s</p>", body),
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

	if err := attributevalue.UnmarshalListOfMaps(cachedData.Items, &items); err != nil {
		return nil, fmt.Errorf("error unmarshalling DynamoDB list of maps: %v", err)
	}

	return items, nil
}

func (h *handler) HandleRequest(ctx context.Context, payload []shiftboard.Shift) (string, error) {
	// End function runtime if payload is empty
	if len(payload) == 0 {
		return "", fmt.Errorf("function payload is empty")
	}

	// Read existing items from DynamoDB table
	dbOutput, err := h.Scan(context.TODO(), h.dbClient, h.tableName)
	if err != nil {
		return "", fmt.Errorf("error scanning DynamoDB table: %v", err)
	}

	// Unmarshal items from DynamoDB to ShiftBoard Shift struct list
	cachedData, err := unmarshalDBItems(dbOutput)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling cached data: %v", err)
	}

	// Write data to DynamoDB table and finish if no cache exists
	if dbOutput.Count == 0 {
		if err = h.writeToDB(h.tableName, payload); err != nil {
			return "", fmt.Errorf("error writing data to DynamoDB table: %v", err)
		}
		return "Success", nil
	}

	// Compare new data with cached data and send notifications on new and updated items
	if err = h.compareData(&payload, &cachedData); err != nil {
		return "", fmt.Errorf("error comparing new data with cache: %v", err)
	}

	// Write new data to DynamoDB table
	if err = h.writeToDB(h.tableName, payload); err != nil {
		return "", fmt.Errorf("error writing data to DynamoDB table: %v", err)
	}

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
