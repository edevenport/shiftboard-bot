package main

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/edevenport/shiftboard-sdk-go"

	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

type MockItem struct {
	*shiftboard.Shift
}

type mockInvokeAPI func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)

type mockPutItemAPI func(ctx context.Context, params *dynamodb.PutItemInput, optsFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)

type mockNewScanPaginatorAPI struct {
	PageNum int
	Pages   []*dynamodb.ScanOutput
}

func (m *mockNewScanPaginatorAPI) HasMorePages() bool {
	return m.PageNum < len(m.Pages)
}

func (m *mockNewScanPaginatorAPI) NextPage(ctx context.Context, f ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	if m.PageNum >= len(m.Pages) {
		return nil, fmt.Errorf("no more pages")
	}

	output := m.Pages[m.PageNum]
	m.PageNum++
	return output, nil
}

func (m mockInvokeAPI) Invoke(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
	return m(ctx, params, optFns...)
}

func (m mockPutItemAPI) PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return m(ctx, params, optFns...)
}

func TestInvoke(t *testing.T) {
	h := handler{}

	cases := []struct {
		client         func(t *testing.T) LambdaInvokeAPI
		functionName   string
		invocationType lambdatypes.InvocationType
		payload        []byte
		expect         *lambda.InvokeOutput
	}{
		{
			client: func(t *testing.T) LambdaInvokeAPI {
				return mockInvokeAPI(func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
					t.Helper()
					if params.FunctionName == nil {
						t.Fatal("expect path to not be nil")
					}
					if e, a := "testFunction", *params.FunctionName; e != a {
						t.Errorf("expect %v, got %v", e, a)
					}
					if params.InvocationType == "" {
						t.Fatal("expect InvocationType to not be empty")
					}
					if e, a := lambdatypes.InvocationTypeEvent, params.InvocationType; e != a {
						t.Errorf("expect %v, got %v", e, a)
					}
					if params.Payload == nil {
						t.Fatal("expect payload not to be nil")
					}
					if e, a := []byte(`{"testkey":"testval"}`), params.Payload; bytes.Compare(e, a) != 0 {
						t.Errorf("expect %v, got %v", e, a)
					}

					return &lambda.InvokeOutput{
						Payload:    []byte(`{"testkey":"testval"}`),
						StatusCode: 200,
					}, nil
				})
			},
			functionName:   "testFunction",
			invocationType: lambdatypes.InvocationTypeEvent,
			payload:        []byte(`{"testkey":"testval"}`),
			expect: &lambda.InvokeOutput{
				Payload:    []byte(`{"testkey":"testval"}`),
				StatusCode: 200,
			},
		},
	}

	for i, tt := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			content, err := h.Invoke(context.TODO(), tt.client(t), tt.functionName, tt.payload)
			if err != nil {
				t.Fatalf("expect no error, got %v", err)
			}
			if e, a := tt.expect.Payload, content.Payload; bytes.Compare(e, a) != 0 {
				t.Errorf("expect %v, got %v", e, a)
			}
			if e, a := tt.expect.StatusCode, content.StatusCode; e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
		})
	}
}

func TestPutItem(t *testing.T) {
	h := handler{}
	item := &MockItem{&shiftboard.Shift{}}
	avItem := item.AttributeValue()

	cases := []struct {
		client    func(t *testing.T) DynamoDBPutItemAPI
		tableName string
		item      map[string]dbtypes.AttributeValue
		expect    *dynamodb.PutItemOutput
	}{
		{
			client: func(t *testing.T) DynamoDBPutItemAPI {
				return mockPutItemAPI(func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
					t.Helper()
					if params.TableName == nil {
						t.Fatal("expect path to not be nil")
					}
					if e, a := "testTable", *params.TableName; e != a {
						t.Errorf("expect %v, got %v", e, a)
					}
					if params.Item == nil {
						t.Fatal("expect item not to be nil")
					}
					if e, a := fmt.Sprint(avItem), fmt.Sprint(params.Item); e != a {
						t.Errorf("expect %v, got %v", e, a)
					}
					return &dynamodb.PutItemOutput{
						Attributes:            map[string]dbtypes.AttributeValue{},
						ConsumedCapacity:      nil,
						ItemCollectionMetrics: nil,
					}, nil
				})
			},
			tableName: "testTable",
			item:      avItem,
			expect: &dynamodb.PutItemOutput{
				Attributes:            map[string]dbtypes.AttributeValue{},
				ConsumedCapacity:      nil,
				ItemCollectionMetrics: nil,
			},
		},
	}

	for i, tt := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			content, err := h.PutItem(context.TODO(), tt.client(t), tt.tableName, tt.item)
			if err != nil {
				t.Fatalf("expect no error, got %v", err)
			}
			if e, a := len(tt.expect.Attributes), len(content.Attributes); e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
		})
	}
}

func TestScanPages(t *testing.T) {
	h := handler{}
	item := MockItem{&shiftboard.Shift{}}

	itemList := []map[string]dbtypes.AttributeValue{}

	pager := &mockNewScanPaginatorAPI{
		Pages: []*dynamodb.ScanOutput{
			{
				Items: append(itemList, item.New().AttributeValue()),
				Count: 1,
			},
			{
				Items: append(itemList, item.New().AttributeValue()),
				Count: 1,
			},
			{
				Items: append(itemList, item.New().AttributeValue()),
				Count: 1,
			},
		},
	}
	objects, err := h.scanPages(context.TODO(), pager)
	if err != nil {
		t.Fatalf("expect no error, got %v", err)
	}
	if expect, actual := 3, len(objects); expect != actual {
		t.Errorf("expect %v, got %v", expect, actual)
	}
}

func TestConstructMessage(t *testing.T) {
	cases := []struct {
		description string
		item        diff
		expect      string
	}{
		{
			description: "addMessage",
			item:        diff{State: "created", Shift: mockShift()},
			expect:      "New shift added",
		},
		{
			description: "updateMessage",
			item:        diff{State: "updated", Shift: mockShift()},
			expect:      "Shift updated",
		},
		{
			description: "emptyMessage",
			item:        diff{},
			expect:      "",
		},
	}

	for _, tt := range cases {
		t.Run(tt.description, func(t *testing.T) {
			result := constructMessage(tt.item)
			if e, a := tt.expect, result; !strings.HasPrefix(a.Subject, e) {
				t.Errorf("expect prefix %v, got %v", e, a.Subject)
			}
		})
	}
}

func TestCompareData(t *testing.T) {
	h := handler{}

	// Mock new data
	newData := []shiftboard.Shift{mockShift()}

	// Mock cache data
	cacheData := make([]shiftboard.Shift, len(newData))
	copy(cacheData, newData)

	// Change "Updated" date to one month prior for cache item
	priorMonth := cacheData[0].Updated.AddDate(0, -1, 0).Format(time.RFC3339)
	cacheData[0].Updated, _ = time.Parse(time.RFC3339, priorMonth)

	cases := []struct {
		description string
		newData     []shiftboard.Shift
		cachedData  []shiftboard.Shift
		expect      string
	}{
		{
			description: "compareCreate",
			newData:     newData,
			cachedData:  []shiftboard.Shift{},
			expect:      "created",
		},
		{
			description: "compareUpdate",
			newData:     newData,
			cachedData:  cacheData,
			expect:      "updated",
		},
	}

	for _, tt := range cases {
		t.Run(tt.description, func(t *testing.T) {
			changeLog := h.compareData(&tt.newData, &tt.cachedData)
			if e, a := tt.expect, changeLog[0].State; e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
		})
	}

}

func TestGetState(t *testing.T) {
	// item := &MockItem{&shiftboard.Shift{}}
	// item.New()

	// shift := *item.Shift
	// cache := mockCache(shift)
	shift := mockShift()
	cache := []shiftboard.Shift{shift}

	priorMonth := shift.Updated.AddDate(0, 1, 0).Format(time.RFC3339)
	shift.Updated, _ = time.Parse(time.RFC3339, priorMonth)

	cases := []struct {
		description string
		shift       shiftboard.Shift
		cache       []shiftboard.Shift
		expect      string
	}{
		{
			description: "itemCreated",
			shift:       shift,
			cache:       []shiftboard.Shift{},
			expect:      "created",
		},
		{
			description: "itemUpdated",
			shift:       shift,
			cache:       cache,
			expect:      "updated",
		},
		{
			description: "itemUnknown",
			shift:       shift,
			cache:       []shiftboard.Shift{shift},
			expect:      "",
		},
	}

	for _, tt := range cases {
		t.Run(tt.description, func(t *testing.T) {
			state := getState(tt.shift, &tt.cache)
			if e, a := tt.expect, state; e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	mockEnv()

	cases := []struct {
		description string
		key         string
		fallback    string
		expect      string
	}{
		{
			description: "envSet",
			key:         "MOCK_ENV",
			fallback:    "notTested",
			expect:      "test",
		},
		{
			description: "envFallback",
			key:         "",
			fallback:    "testFallback",
			expect:      "testFallback",
		},
	}

	for _, tt := range cases {
		t.Run(tt.description, func(t *testing.T) {
			result := getEnv(tt.key, tt.fallback)
			if e, a := tt.expect, result; e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
		})
	}
}

func TestAddItemTTL(t *testing.T) {
	expect := int64(1657886400)
	result := addItemTTL(mockShift())

	if (ShiftExt{} == result) {
		t.Errorf("expect struct not to be empty")
	}

	if e, a := expect, result.TTL; e != a {
		t.Errorf("expect %v, got %v", e, a)
	}
}

func mockEnv() {
	err := os.Setenv("MOCK_ENV", "test")
	if err != nil {
		panic(err)
	}
}

func (m *MockItem) AttributeValue() map[string]dbtypes.AttributeValue {
	av, err := attributevalue.MarshalMap(m)
	if err != nil {
		panic(err)
	}

	return av
}

func (m *MockItem) New() *MockItem {
	createTime, _ := time.Parse(time.RFC3339, "2022-04-18T12:00:00Z")
	updateTime, _ := time.Parse(time.RFC3339, "2022-05-11T12:00:00Z")

	m.ID = randomID()
	m.Name = randomString()
	m.StartDate = "2022-06-15T12:00:00"
	m.EndDate = "2022-06-15T12:00:00"
	m.Created = createTime
	m.Updated = updateTime

	return m
}

func mockShift() shiftboard.Shift {
	item := &MockItem{&shiftboard.Shift{}}
	item.New()

	return *item.Shift
}

func randomID() string {
	rand.Seed(time.Now().UnixNano())

	min := 100000000
	max := 999999999
	id := min + rand.Intn(max-min)

	return strconv.Itoa(id)
}

func randomString() string {
	rand.Seed(time.Now().UnixNano())

	b := make([]byte, 24)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}

	return string(b)
}
