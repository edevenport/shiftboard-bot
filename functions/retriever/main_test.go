package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

type mockGetParametersByPathAPI func(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)

type mockInvokeAPI func(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error)

func (m mockGetParametersByPathAPI) GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	return m(ctx, params, optFns...)
}

func (m mockInvokeAPI) Invoke(ctx context.Context, params *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
	return m(ctx, params, optFns...)
}

func TestGetParametersByPath(t *testing.T) {
	h := handler{}

	cases := []struct {
		client         func(t *testing.T) SSMGetParametersByPathAPI
		path           string
		withDecryption bool
		expect         *ssm.GetParametersByPathOutput
	}{
		{
			client: func(t *testing.T) SSMGetParametersByPathAPI {
				return mockGetParametersByPathAPI(func(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
					t.Helper()
					if params.Path == nil {
						t.Fatal("expect path to not be nil")
					}
					if e, a := "/path/to/key", *params.Path; e != a {
						t.Errorf("expect %v, got %v", e, a)
					}
					if params.WithDecryption == false {
						t.Fatal("expect WithDecryption to not be false")
					}
					if e, a := true, params.WithDecryption; e != a {
						t.Errorf("expect %v, got %v", e, a)
					}

					return &ssm.GetParametersByPathOutput{
						Parameters: []ssmtypes.Parameter{{Value: aws.String("test")}},
					}, nil
				})
			},
			path:           "/path/to/key",
			withDecryption: true,
			expect: &ssm.GetParametersByPathOutput{
				Parameters: []ssmtypes.Parameter{{Value: aws.String("test")}},
			},
		},
	}

	for i, tt := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			ctx := context.TODO()
			content, err := h.GetParametersByPath(ctx, tt.client(t), tt.path, tt.withDecryption)
			if err != nil {
				t.Fatalf("expect no error, got %v", err)
			}
			if e, a := *tt.expect.Parameters[0].Value, *content.Parameters[0].Value; e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
		})
	}
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
			ctx := context.TODO()
			content, err := h.Invoke(ctx, tt.client(t), tt.functionName, tt.payload)
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

func TestParseParameters(t *testing.T) {
	cases := []struct {
		description    string
		output         *ssm.GetParametersByPathOutput
		expectEmail    string
		expectPassword string
		expectErr      error
	}{
		{
			description:    "checkParameters",
			output:         mockParametersOutput(true),
			expectEmail:    "user@example.com",
			expectPassword: "password123",
			expectErr:      nil,
		},
		{
			description:    "checkEmptyParameters",
			output:         mockParametersOutput(false),
			expectEmail:    "",
			expectPassword: "",
			expectErr:      errors.New("no parameters returned from SSM parameter store"),
		},
	}

	for _, tt := range cases {
		t.Run(tt.description, func(t *testing.T) {
			email, password, err := parseParameters(tt.output)
			if e, a := tt.expectErr, err; a != nil && e.Error() != a.Error() {
				t.Errorf("expect %v, got %v", e, a)
			}
			if e, a := tt.expectEmail, email; e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
			if e, a := tt.expectPassword, password; e != a {
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

// mockParametersOutput returns mock parameters if 'params' bool is true, otherwise
// returns an empty parameters slice if false.
func mockParametersOutput(params bool) *ssm.GetParametersByPathOutput {
	var parameters []ssmtypes.Parameter

	if params {
		parameters = append(parameters, ssmtypes.Parameter{
			Name:  aws.String("/shiftboard/api/email"),
			Value: aws.String("user@example.com"),
		})

		parameters = append(parameters, ssmtypes.Parameter{
			Name:  aws.String("/shiftboard/api/password"),
			Value: aws.String("password123"),
		})
	}

	return &ssm.GetParametersByPathOutput{
		Parameters: parameters,
	}
}

func mockEnv() {
	err := os.Setenv("MOCK_ENV", "test")
	if err != nil {
		panic(err)
	}
}
