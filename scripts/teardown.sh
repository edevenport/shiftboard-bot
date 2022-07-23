#!/bin/bash
set -euo pipefail

# Stop Localstack

#######################################
# Check whether Localstack is running.
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
# Stop Localstack.
# Arguments:
#   None
#######################################
function stop_localstack() {
    if localstack_running; then
        localstack stop
    fi
}

#######################################
# Main function.
# Arguments:
#   None
#######################################
function main() {
    echo "Stopping Localstack"
    stop_localstack
}

main "$@"
