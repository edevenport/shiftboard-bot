package main

import (
	"context"
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/smithy-go/middleware"
	"github.com/edevenport/shiftboard-sdk-go"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

type MockItem struct {
	*shiftboard.Shift
}

type mockGetParametersByPathAPI func(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)

type mockSendEmailAPI func(ctx context.Context, params *ses.SendEmailInput, optFns ...func(*ses.Options)) (*ses.SendEmailOutput, error)

func (m mockGetParametersByPathAPI) GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	return m(ctx, params, optFns...)
}

func (m mockSendEmailAPI) SendEmail(ctx context.Context, params *ses.SendEmailInput, optFns ...func(*ses.Options)) (*ses.SendEmailOutput, error) {
	return m(ctx, params, optFns...)
}

func TestGetParametersByPath(t *testing.T) {
	cases := []struct {
		client         func(t *testing.T) SSMGetParametersByPathAPI
		path           string
		withDecryption bool
		expect         *ssm.GetParametersByPathOutput
	}{
		{
			client: func(t *testing.T) SSMGetParametersByPathAPI {
				return mockGetParametersByPathAPI(func(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
					if params.Path == nil {
						t.Fatal("expect path to not be nil")
					}
					if e, a := "/path/to/key", *params.Path; e != a {
						t.Errorf("expect %v, got %v", e, a)
					}
					if params.WithDecryption == true {
						t.Fatal("expect WithDecryption to not be false")
					}
					if e, a := false, params.WithDecryption; e != a {
						t.Errorf("expect %v, got %v", e, a)
					}

					return &ssm.GetParametersByPathOutput{
						Parameters: []types.Parameter{{Value: aws.String("test")}},
					}, nil
				})
			},
			path:           "/path/to/key",
			withDecryption: false,
			expect: &ssm.GetParametersByPathOutput{
				Parameters: []types.Parameter{{Value: aws.String("test")}},
			},
		},
	}

	for i, tt := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			ctx := context.TODO()
			output, err := GetParametersByPath(ctx, tt.client(t), tt.path, tt.withDecryption)
			if err != nil {
				t.Fatalf("expect no error, got %v", err)
			}
			if e, a := *tt.expect.Parameters[0].Value, *output.Parameters[0].Value; e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
		})
	}
}

func TestConstructMessage(t *testing.T) {
	cases := []struct {
		description string
		item        Diff
		expect      string
	}{
		{
			description: "newMessage",
			item:        Diff{State: "created", Shift: mockShift()},
			expect:      "New shift added",
		},
		{
			description: "updateMessage",
			item:        Diff{State: "updated", Shift: mockShift()},
			expect:      "Shift updated",
		},
		{
			description: "emptyMessage",
			item:        Diff{},
			expect:      "",
		},
	}

	for _, tt := range cases {
		t.Run(tt.description, func(t *testing.T) {
			result := constructMessage(&tt.item)
			if e, a := tt.expect, result; !strings.HasPrefix(a.Subject, e) {
				t.Errorf("expect prefix %v, got %v", e, a.Subject)
			}
		})
	}
}

func TestFormatDate(t *testing.T) {
	shift := mockShift()
	created := shift.Created.Format(time.RFC1123)
	updated := shift.Updated.Format(time.RFC1123)

	cases := []struct {
		description string
		item        Diff
		expect      string
	}{
		{
			description: "createdFormat",
			item:        Diff{State: "created", Shift: mockShift()},
			expect:      created,
		},
		{
			description: "updatedFormat",
			item:        Diff{State: "updated", Shift: mockShift()},
			expect:      updated,
		},
	}

	for _, tt := range cases {
		t.Run(tt.description, func(t *testing.T) {
			result := formatDate(&tt.item)
			if result == "" {
				t.Fatal("expect result to not be empty")
			}
			if e, a := tt.expect, result; e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
		})
	}
}

func TestSendEmail(t *testing.T) {
	messageID := "50632886-158d-4f8b-abf8-d74649e92d7b"

	cases := []struct {
		client    func(t *testing.T) SESSendEmailAPI
		sender    string
		recipient string
		msg       Message
		expect    *ses.SendEmailOutput
	}{
		{
			client: func(t *testing.T) SESSendEmailAPI {
				return mockSendEmailAPI(func(ctx context.Context, params *ses.SendEmailInput, optFns ...func(*ses.Options)) (*ses.SendEmailOutput, error) {
					t.Helper()
					if params.Destination == nil {
						t.Fatal("expect destination to not be nil")
					}
					if e, a := "user@example.com", params.Destination.ToAddresses[0]; e != a {
						t.Errorf("expect %v, got %v", e, a)
					}
					if params.Source == nil {
						t.Fatal("expect source to not be nil")
					}
					if e, a := "no-reply@example.com", *params.Source; e != a {
						t.Errorf("expect %v, got %v", e, a)
					}
					if *params.Message.Subject.Data == "" {
						t.Fatal("expect Subject to not be empty")
					}
					if e, a := "test", *params.Message.Subject.Data; e != a {
						t.Errorf("expect %v, got %v", e, a)
					}
					if *params.Message.Body.Text.Data == "" {
						t.Fatal("expect text message body not to be empty")
					}
					if e, a := "text message", *params.Message.Body.Text.Data; e != a {
						t.Errorf("expect %v, got %v", e, a)
					}
					if *params.Message.Body.Html.Data == "" {
						t.Fatal("expect HTML message body not to be empty")
					}
					if e, a := "html message", *params.Message.Body.Html.Data; e != a {
						t.Errorf("expect %v, got %v", e, a)
					}

					return &ses.SendEmailOutput{
						MessageId:      aws.String(messageID),
						ResultMetadata: middleware.Metadata{},
					}, nil
				})
			},
			sender:    "no-reply@example.com",
			recipient: "user@example.com",
			msg: Message{
				Subject:  "test",
				TextBody: "text message",
				HtmlBody: "html message",
			},
			expect: &ses.SendEmailOutput{
				MessageId:      aws.String(messageID),
				ResultMetadata: middleware.Metadata{},
			},
		},
	}

	for i, tt := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			ctx := context.TODO()
			output, err := SendEmail(ctx, tt.client(t), tt.sender, tt.recipient, tt.msg)
			if err != nil {
				t.Fatalf("expect no error, got %v", err)
			}
			if e, a := *tt.expect.MessageId, *output.MessageId; e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
		})
	}
}

func TestParseParameters(t *testing.T) {
	cases := []struct {
		description     string
		output          *ssm.GetParametersByPathOutput
		expectSender    string
		expectRecipient string
		expectErr       error
	}{
		{
			description:     "checkParameters",
			output:          mockParametersOutput(true),
			expectSender:    "no-reply@example.com",
			expectRecipient: "user@example.com",
			expectErr:       nil,
		},
		{
			description:     "checkEmptyParameters",
			output:          mockParametersOutput(false),
			expectSender:    "",
			expectRecipient: "",
			expectErr:       errors.New("no parameters returned from SSM parameter store"),
		},
	}

	for _, tt := range cases {
		t.Run(tt.description, func(t *testing.T) {
			sender, recipient, err := parseParameters(tt.output)
			if e, a := tt.expectErr, err; a != nil && e.Error() != a.Error() {
				t.Errorf("expect %v, got %v", e, a)
			}
			if e, a := tt.expectSender, sender; e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
			if e, a := tt.expectRecipient, recipient; e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
		})
	}
}

// mockParametersOutput returns mock parameters if 'params' bool is true, otherwise
// returns an empty parameters slice if false.
func mockParametersOutput(params bool) *ssm.GetParametersByPathOutput {
	var parameters []types.Parameter

	if params {
		parameters = append(parameters, types.Parameter{
			Name:  aws.String("/shiftboard/notifications/sender"),
			Value: aws.String("no-reply@example.com"),
		})

		parameters = append(parameters, types.Parameter{
			Name:  aws.String("/shiftboard/notifications/recipient"),
			Value: aws.String("user@example.com"),
		})
	}

	return &ssm.GetParametersByPathOutput{
		Parameters: parameters,
	}
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
