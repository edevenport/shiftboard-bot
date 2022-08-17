package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/edevenport/shiftboard-sdk-go"

	runtime "github.com/aws/aws-lambda-go/lambda"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

const (
	dbPageCount  = 100
	dbBatchCount = 25
)

type handler struct {
	notificationFunction string
	tableName            string
	dbClient             *dynamodb.Client
	lambdaClient         *lambda.Client
}

type diff struct {
	State string
	Shift shiftboard.Shift
}

type ShiftExt struct {
	shiftboard.Shift
	TTL int64
}

type message struct {
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

type DynamoDBBatchWriteItemAPI interface {
	BatchWriteItem(ctx context.Context,
		params *dynamodb.BatchWriteItemInput,
		optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
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

func (h *handler) BatchWriteItem(ctx context.Context, api DynamoDBBatchWriteItemAPI, requestItems map[string][]dbtypes.WriteRequest) (*dynamodb.BatchWriteItemOutput, error) {
	return api.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
		RequestItems: requestItems,
	})
}

func (h *handler) Invoke(ctx context.Context, api LambdaInvokeAPI, functionName string, payload []byte) (*lambda.InvokeOutput, error) {
	return api.Invoke(ctx, &lambda.InvokeInput{
		FunctionName:   aws.String(functionName),
		InvocationType: lambdatypes.InvocationTypeEvent,
		Payload:        payload,
	})
}

func (h *handler) compareData(newData *[]shiftboard.Shift, cachedData *[]shiftboard.Shift) (changeLog []diff) {
	for i := 0; i < len(*newData); i++ {
		shift := (*newData)[i]
		diff := diff{}

		if state := getState(shift, cachedData); state != "" {
			diff.State = state
			diff.Shift = shift
			changeLog = append(changeLog, diff)
		}
	}

	return changeLog
}

func (h *handler) writeItemToDB(tableName string, item shiftboard.Shift) error {
	itemExt := addItemTTL(item)

	av, err := attributevalue.MarshalMap(itemExt)
	if err != nil {
		return fmt.Errorf("error marshalling DynamoDB attribute value map: %v", err)
	}

	_, err = h.PutItem(context.TODO(), h.dbClient, tableName, av)
	if err != nil {
		return fmt.Errorf("error calling DynamoDB PutItem: %v", err)
	}

	fmt.Printf("Successfully added '%s' to table %s\n", itemExt.Name, tableName)

	return nil
}

func (h *handler) writePayloadBatch(payload []shiftboard.Shift) error {
	writeRequestList := []dbtypes.WriteRequest{}

	for _, item := range payload {
		writeRequest, err := constructWriteRequest(item)
		if err != nil {
			return fmt.Errorf("unable to construct batch write request: %v", err)
		}

		writeRequestList = append(writeRequestList, *writeRequest)
	}

	batchRequest := map[string][]dbtypes.WriteRequest{h.tableName: writeRequestList}

	output, err := h.BatchWriteItem(context.TODO(), h.dbClient, batchRequest)
	if err != nil {
		return fmt.Errorf("error writing batch items to DynamoDB: %v", err)
	}

	fmt.Printf("BatchWriteItem Output: %+v\n", output)

	if len(output.UnprocessedItems) != 0 {
		return fmt.Errorf("identified unprocessed batch items")
	}

	return nil
}

func (h *handler) writeAllToDB(tableName string, payload []shiftboard.Shift) error {
	fmt.Printf("Total item count: %d\n", len(payload))
	batch := dbBatchCount

	for start := 0; start < len(payload); start += batch {
		end := start + batch
		if end > len(payload) {
			end = len(payload)
		}

		fmt.Printf("Batch item count: %d\n", len(payload[start:end]))

		err := h.writePayloadBatch(payload[start:end])
		if err != nil {
			return fmt.Errorf("error writing batch payload")
		}
	}

	return nil
}

func (h *handler) sendNotification(msg message) error {
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

	// Write payload to DynamoDB table if no cache already exists and finish
	if len(cachedData) == 0 {
		if err := h.writeAllToDB(h.tableName, payload); err != nil {
			return "", fmt.Errorf("error writing data to DynamoDB table: %v", err)
		}
		return "Success", nil
	}

	// Compare payload with enteries cached in DynamoDB
	for _, item := range h.compareData(&payload, &cachedData) {
		msg := constructMessage(item)

		if err := h.sendNotification(msg); err != nil {
			return "", fmt.Errorf("error sending notification: %v", err)
		}

		if err := h.writeItemToDB(h.tableName, item.Shift); err != nil {
			return "", fmt.Errorf("error writing shift to DynamoDB: %v", err)
		}
	}

	return "Success", nil
}

func constructWriteRequest(item shiftboard.Shift) (*dbtypes.WriteRequest, error) {
	itemExt := addItemTTL(item)

	av, err := attributevalue.MarshalMap(itemExt)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal map to DynamoDB attribute values: %v", err)
	}

	return &dbtypes.WriteRequest{
		PutRequest: &dbtypes.PutRequest{
			Item: av,
		},
	}, nil
}

func constructMessage(item diff) (msg message) {
	shift := item.Shift

	if item.State == "created" {
		msg.Subject = fmt.Sprintf("New shift added: %s", shift.Name)
		msg.TextBody = fmt.Sprintf("Shift has been added for '%s' on %s", shift.Name, shift.Created)
	}

	if item.State == "updated" {
		msg.Subject = fmt.Sprintf("Shift updated: %s", shift.Name)
		msg.TextBody = fmt.Sprintf("Shift for '%s' was updated on %s", shift.Name, shift.Updated)
	}

	msg.HTMLBody = fmt.Sprintf("<p>%s</p>", msg.TextBody)

	return msg
}

func getState(shift shiftboard.Shift, cache *[]shiftboard.Shift) string {
	found := false
	updated := false

	for _, c := range *cache {
		if c.ID == shift.ID {
			found = true
			if c.Updated.Before(shift.Updated) {
				updated = true
			}
			break
		}
	}

	if !found {
		return "created"
	}

	if updated {
		return "updated"
	}

	return ""
}

func addItemTTL(item shiftboard.Shift) ShiftExt {
	// Fix date string and convert to time.Time type
	endDate, _ := time.Parse(time.RFC3339, item.EndDate+"Z")

	// Set DynamoDB TTL one month after the shift end date
	ttl := endDate.AddDate(0, 1, 0)

	// Extend shift object with TTL field
	var shift ShiftExt
	shift.Shift = item
	shift.TTL = ttl.Unix()

	return shift
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
