#!/bin/bash
set -euo pipefail

# Start Localstack and seed with data for testing. Optional seed file 
# can be passed as an argument to override defaults.

# Global variables
APP_NAME="shiftboard-bot"
ENV="test"

# AWS environment variables
AWS_REGION="us-west-2"
AWS_S3_BUCKET="shiftboard-bot"
AWS_ENDPOINT_URL="http://localhost:4566"
AWS_LOCAL="true"

# Exported Localstack environment variables
export EAGER_SERVICE_LOADING=1
export SERVICES="ssm,dynamodb,lambda,iam,kms,cloudformation"
export DEBUG=1
export DEFAULT_REGION="$AWS_REGION"
export LAMBDA_DOCKER_FLAGS="-e AWS_SAM_LOCAL=$AWS_LOCAL"

#######################################
# Check whether AWS S3 bucket already exists.
# Globals:
#   AWS_S3_BUCKET
#   AWS_ENDPOINT_URL
# Arguments:
#   None
# Returns:
#   0 if bucket exists, non-zero if bucket does not exist.
#######################################
function bucket_exists() {
    local rc=0
    aws s3api head-bucket \
        --bucket "$AWS_S3_BUCKET" \
        --endpoint-url "$AWS_ENDPOINT_URL" > /dev/null 2>&1 \
        || rc="$?"
    return "$rc"
}

#######################################
# Check whether Localstack is running locally
# Arguments:
#   None
# Returns:
#   0 if Localstack is running, non-zero if not running
#######################################
function localstack_running() {
    local rc=0
    localstack status services > /dev/null 2>&1 || rc="$?"
    return "$rc"
}

#######################################
# Start Localstack and wait for response if not already running.
# Arguments:
#   None
#######################################
function start_localstack() {
    if ! localstack_running; then
        localstack start --no-banner -d
        localstack wait
    fi
}

#######################################
# Create an AWS S3 bucket if the bucket does not already exist.
# Globals:
#   AWS_S3_BUCKET
#   AWS_REGION
#   AWS_ENDPOINT_URL
#######################################
function create_bucket() {
    if ! bucket_exists; then
        aws s3api create-bucket \
            --bucket "$AWS_S3_BUCKET" \
            --create-bucket-configuration LocationConstraint="$AWS_REGION" \
            --endpoint-url "$AWS_ENDPOINT_URL"

        tags="TagSet=[{Key=App,Value=${APP_NAME}},{Key=Environment,Value=${ENV}}]"

        aws s3api put-bucket-tagging \
            --bucket "$AWS_S3_BUCKET" \
            --tagging "$tags" \
            --endpoint-url "$AWS_ENDPOINT_URL"
    fi
}

#######################################
# Add parameter to the AWS SSM Parameter Store
# Globals:
#   AWS_ENDPOINT_URL
# Arguments:
#   Parameter name, parameter value, optional "secure" string
#######################################
function add_parameter() {
    ssm_args=(
        ssm
        put-parameter
        --name "$1"
        --value "$2"
	--overwrite
        --endpoint-url "$AWS_ENDPOINT_URL"
    )

    if [ "${3-}" == "secure" ]; then
        ssm_args+=(--type SecureString)
    fi

    aws "${ssm_args[@]}"
}

#######################################
# Set default seed variables and override if available.
# Globals:
#   SHIFTBOARD_USERNAME
#   SHIFTBOARD_PASSWORD
#   SMTP_SENDER
#   SMTP_RECIPIENT
# Arguments:
#   Path to override file
#######################################
function load_seed_vars {
    SHIFTBOARD_USERNAME="${SHIFTBOARD_USERNAME:-testuser}"
    SHIFTBOARD_PASSWORD="${SHIFTBOARD_PASSWORD:-testpassword}"
    STATE_FILTER="${STATE_FILTER:-IL,Illinois}"
    SMTP_SENDER="${SMTP_SENDER:-no-reply@example.com}"
    SMTP_RECIPIENT="${SMTP_RECIPIENT:-john.doe@example.com,jane.doe@example.com}"

    if [ -f "${1-}" ]; then
        # shellcheck disable=SC1090
        source "$1"
    fi
}

#######################################
# Main function.
# Globals:
#   SHIFTBOARD_USERNAME
#   SHIFTBOARD_PASSWORD
#   SHIFTBOARD_SENDER
#   SHIFTBOARD_RECIPIENT
# Arguments:
#   Optional path to override file
#######################################
function main() {
    echo "Start Localstack"
    start_localstack

    echo "Create AWS S3 bucket"
    create_bucket

    echo "Load variables from file or use defaults"
    load_seed_vars "${1-}"

    echo "Seed SSM Parameter Store"
    add_parameter "/shiftboard/api/email" "$SHIFTBOARD_USERNAME" "secure"
    add_parameter "/shiftboard/api/password" "$SHIFTBOARD_PASSWORD" "secure"
    add_parameter "/shiftboard/api/state_filter" "$STATE_FILTER"
    add_parameter "/shiftboard/notifications/sender" "$SMTP_SENDER"
    add_parameter "/shiftboard/notifications/recipient" "$SMTP_RECIPIENT"

    echo "Verify email identity: $SMTP_SENDER"
    aws ses verify-email-identity \
        --email-address "$SMTP_SENDER" \
        --endpoint-url "$AWS_ENDPOINT_URL"
}

main "$@"
