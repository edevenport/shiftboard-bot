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

const dbPageCount = 1

type handler struct {
	notificationFunction string
	tableName            string
	dbClient             *dynamodb.Client
	lambdaClient         *lambda.Client
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

type DynamoDBNewScanPaginatorAPI interface {
	HasMorePages() bool
	NextPage(context.Context, ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
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

			if err := h.sendNotification(subject, msgBody); err != nil {
				return fmt.Errorf("Error sending 'new shift' notification: %v", err)
			}

			if err := h.writeItemToDB(h.tableName, shift); err != nil {
				return fmt.Errorf("Unable to write new shift to DynamoDB: %v", err)
			}
		}

		if updated {
			subject = fmt.Sprintf("Shift updated: %s", shift.Name)
			msgBody = fmt.Sprintf("Shift for '%s' was updated on %s", shift.Name, shift.Updated)

			if err := h.sendNotification(subject, msgBody); err != nil {
				return fmt.Errorf("Error sending 'updated shift' notification: %v", err)
			}

			if err := h.writeItemToDB(h.tableName, shift); err != nil {
				return fmt.Errorf("Unable to write updated shift to DynamoDB: %v", err)
			}
		}
	}

	return nil
}

func (h *handler) writeItemToDB(tableName string, item shiftboard.Shift) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("error marshalling DynamoDB attribuet value map: %v", err)
	}

	if _, err = h.PutItem(context.TODO(), h.dbClient, tableName, av); err != nil {
		return fmt.Errorf("error calling DynamoDB PutItem: %v", err)
	}

	fmt.Printf("Successfully added '%s' to table %s\n", item.Name, tableName)

	return nil
}

func (h *handler) writeToDB(tableName string, payload []shiftboard.Shift) error {
	for _, item := range payload {
		if err := h.writeItemToDB(tableName, item); err != nil {
			return err
		}
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

	_, err = h.Invoke(context.TODO(), h.lambdaClient, h.notificationFunction, payload)
	if err != nil {
		return fmt.Errorf("error invoking function '%v': %v", h.notificationFunction, err)
	}

	return nil
}

func (h *handler) scanPages(ctx context.Context, pager DynamoDBNewScanPaginatorAPI) ([]shiftboard.Shift, error) {
	var list []shiftboard.Shift
	page := 1

	for pager.HasMorePages() {
		fmt.Println("page ", page)
		output, err := pager.NextPage(ctx)
		if err != nil {
			return list, err
		}

		var pItems []shiftboard.Shift
		err = attributevalue.UnmarshalListOfMaps(output.Items, &pItems)
		if err != nil {
			return list, err
		}

		list = append(list, pItems...)
		page++
	}
	return list, nil
}

func (h *handler) HandleRequest(ctx context.Context, payload []shiftboard.Shift) (string, error) {
	// Initialize DynamoDB scan paginator
	p := dynamodb.NewScanPaginator(h.dbClient, &dynamodb.ScanInput{
		TableName: aws.String(h.tableName),
		Limit:     aws.Int32(dbPageCount),
	})

	// Read existing cached data from DynamoDB table
	cachedData, err := h.scanPages(context.TODO(), p)
	if err != nil {
		return "", fmt.Errorf("error reading data from DynamoDB table: %v", err)
	}

	// Write data to DynamoDB table and finish if no cache exists
	if len(cachedData) == 0 {
		if err := h.writeToDB(h.tableName, payload); err != nil {
			return "", fmt.Errorf("error writing data to DynamoDB table: %v", err)
		}
		return "Success", nil
	}

	// Compare new data with cached data and send notifications on new and updated items
	if err := h.compareData(&payload, &cachedData); err != nil {
		return "", fmt.Errorf("error comparing new data with cache: %v", err)
	}

	return "Success", nil
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
		notificationFunction: getEnv("NOTIFICATION_FUNCTION", "NotificationFunction"),
		tableName:            os.Getenv("TABLE_NAME"),
		dbClient:             dynamodb.NewFromConfig(cfg),
		lambdaClient:         lambda.NewFromConfig(cfg),
	}

	runtime.Start(h.HandleRequest)
}
