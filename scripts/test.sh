#!/bin/bash
set -euo pipefail

ENDPOINT_URL="http://localhost:4566"
OUT_FILE="output.txt"
TABLE_NAME="shiftboard-bot"

RED=$(tput setaf 1)
GREEN=$(tput setaf 2)
YELLOW=$(tput setaf 3)
RED=$(tput setaf 1)
NOCOLOR=$(tput sgr0)

function random_item_id() {
    local item_list item_count rand

    item_list=$(aws dynamodb scan --table-name "$TABLE_NAME" --endpoint-url "$ENDPOINT_URL")
    item_count=$(jq '.Items | length' <<< "$item_list")

    if [ "$item_count" -eq 0 ]; then
        exit 1
    fi

    rand="$((RANDOM % item_count + 1 ))"

    jq -r ".Items[$rand] | .ID.S" <<< "$item_list"
}

function get_function_name() {
    aws lambda list-functions \
        --endpoint-url "$ENDPOINT_URL" | \
        jq -r '.Functions[] | select(.Handler=="retriever") | .FunctionName'
}

function invoke_function() {
    local function_name="$1"

    local output content status_code log_result

    output=$(aws lambda invoke \
        --function-name "$function_name" \
        --endpoint-url "$ENDPOINT_URL" \
        "$OUT_FILE")

    read -r status_code log_result <<< "$(echo "$output" | \
        jq -r '. | [.StatusCode, .LogResult] | @tsv')"

    content=$(cat $OUT_FILE)

    printf "Status code: "
    if [ "$status_code" -eq 200 ]; then
        printf "%s%s%s\n" "$GREEN" "SUCCESS ($status_code)" "$NOCOLOR"
    else
        printf "%s%s%s\n" "$RED" "FAILED ($status_code)" "$NOCOLOR"
        printf "%s\n" "$log_result"
    fi

    printf "Output: "
    if [ "$content" == '"Success"' ]; then
        printf "%s%s%s\n" "$GREEN" "$content" "$NOCOLOR"
    else
        printf "%s%s%s\n" "$RED" "$content" "$NOCOLOR"
    fi

    sleep 10
}

function wait_for_messages() {
    local msg_count timeout

    msg_count=0
    timeout=0

    until [[ "$msg_count" -eq "$1" || "$timeout" -eq 10 ]]; do
        msg_count=$(curl -s "${ENDPOINT_URL}/_localstack/ses" | jq '.messages | length')
        timeout=$(( timeout + 1 ))
        sleep 1
    done
}

# Delete random item from DynamoDB
function simulate_new_shift() {
    local item_id item_name

    item_id=$(random_item_id)
    item_name=$(aws dynamodb delete-item \
        --table-name "$TABLE_NAME" \
        --key "{\"ID\": {\"S\": \"$item_id\"}}" \
        --return-values ALL_OLD \
        --endpoint-url "$ENDPOINT_URL" | \
        jq -r '.Attributes.Name.S')

    echo "$item_name"
}

# Update random item in DynamoDB
function simulate_update_shift() {
    local item_id item_name

    item_id=$(random_item_id)
    item_name=$(aws dynamodb update-item \
        --table-name "$TABLE_NAME" \
        --key "{\"ID\": {\"S\": \"$item_id\"}}" \
        --update-expression "SET Updated = :u" \
        --expression-attribute-values '{":u": { "S": "2022-01-01T00:00:00Z"}}' \
        --return-values ALL_NEW \
        --endpoint-url "$ENDPOINT_URL" | \
        jq -r '.Attributes.Name.S')

    echo "$item_name"
}

function check_message() {
    local shift_name="$1"
    local rc=0

    curl -s "${ENDPOINT_URL}/_localstack/ses" | \
        jq -e \
        --arg shift_name "$shift_name" \
        '.messages[] | select(.Subject | contains($shift_name))' > /dev/null 2>&1 || rc="$?"

    if [ "$rc" -eq 0 ]; then
        printf "Message delivery: %sSUCCESS%s\n" "$GREEN" "$NOCOLOR"
    else
        printf "Message delivery test: %sFAILED%s\n" "$RED" "$NOCOLOR"
    fi
}

function repeat() {
    local start end str range

    start=1
    end="${1:-10}"
    str="${2:-=}"
    range="$(seq "$start" "$end")"

    for _ in $range; do echo -n "$str"; done
}

function print_header() {
    local str="$1"
    local str_length="${#str}"

    printf "\n%s%s\n%s%s\n" "$YELLOW" "$str" "$(repeat "$str_length")" "$NOCOLOR"
}

function run_test() {
    local setup_cmd="$1"
    local msg_count="$2"

    local shift_name

    # Setup test environment for test
    shift_name="$($setup_cmd)"

    # Invoke Lambda function
    invoke_function "$function_name"

    # Wait for specified message count
    wait_for_messages "$msg_count"

    # Check SES messages for shift by subject
    check_message "$shift_name"
}

function stack_created() {
    aws cloudformation describe-stacks \
        --endpoint-url "$ENDPOINT_URL" | \
        jq -e '.Stacks[0].StackStatus' > /dev/null 2>&1
}

function main() {
    samlocal build

    if ! stack_created; then
        samlocal deploy --no-progressbar
    else
        samlocal package
    fi

    echo "Retrieve Lambda function name"
    function_name=$(get_function_name)

    print_header "Invoke Lambda: $function_name"
    invoke_function "$function_name"

    print_header "Test new shift"
    run_test "simulate_new_shift" 1

    print_header "Test update shift"
    run_test "simulate_update_shift" 2

    print_header "SES messages"
    curl -s "$ENDPOINT_URL/_localstack/ses" | jq '.messages[] | {Id, Subject}'
}

main "$@"
