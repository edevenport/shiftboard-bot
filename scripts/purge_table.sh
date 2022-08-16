#!/bin/bash
set -euo pipefail

# Delete and recreate a local DynamoDB table. Used for managing local
# test environment.

AWS_ENDPOINT_URL="http://localhost:4566"
SCHEMA_FILE="/tmp/schema.json"

function main() {
    local table_name

    # Check for script table name argument.
    if [ "$#" -eq 1 ] && [ -n "$1" ]; then
        table_name="$1"
    else
        echo "Missing DynamoDB table name argument"
        exit 1
    fi

    # Save DynamoDB table schema.
    aws dynamodb describe-table \
        --table-name "$table_name" \
	--endpoint-url "$AWS_ENDPOINT_URL" | \
	jq '.Table | del(.TableId, .TableArn, .ItemCount, .TableSizeBytes, .CreationDateTime, .TableStatus, .ProvisionedThroughput.NumberOfDecreasesToday, .ProvisionedThroughput.LastDecreaseDateTime, .ProvisionedThroughput.LastIncreaseDateTime)' > /tmp/schema.json

    # Delete DynamoDB table.
    aws dynamodb delete-table \
        --table-name "$table_name" \
        --endpoint-url "$AWS_ENDPOINT_URL"

    # Create DynamoDB table from schema file.
    aws dynamodb create-table \
        --cli-input-json "file://$SCHEMA_FILE" \
       	--endpoint-url "$AWS_ENDPOINT_URL"
}

main "$@"
