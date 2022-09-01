![CI](https://github.com/edevenport/shiftboard-bot/actions/workflows/main.yml/badge.svg)

## shiftboard-bot

### Bot Configuration

The bot configuration is stored in the AWS SSM Parameter Store.

| Name | Description |
| ---- | ----------- |
| `/shiftboard/api/email` | ShiftBoard API username |
| `/shiftboard/api/password` | ShiftBoard API password (encrypted) |
| `/shiftboard/api/state_filter` | Optional comma delimited list of states (`WA,Washington`) for filtering API results |
| `/shiftboard/notification/sender` | AWS SES verified sender email address |
| `/shiftboard/notification/recipients` | AWS SES verified comma delimited recipient email addresses |

### Deploy to Localstack

    ./scripts/teardown.sh && ./scripts/setup.sh && localstack logs -f

    ./scripts/test.sh

### Deploy to Production

    sam build && sam deploy --config-env prod
