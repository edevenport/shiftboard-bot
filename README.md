![CI](https://github.com/edevenport/shiftboard-bot/actions/workflows/main.yml/badge.svg)

## shiftboard-bot

### Deploy to Localstack

    ./scripts/teardown.sh && ./scripts/setup.sh && localstack logs -f

    ./scripts/test.sh

### Deploy to Production

    sam build && sam deploy --config-env prod
