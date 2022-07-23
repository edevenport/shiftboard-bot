lint:
	yamllint template.yaml
	sam validate
	cd functions/retriever && golangci-lint run
	cd functions/processor && golangci-lint run
	cd functions/notification && golangci-lint run
	shellcheck ./scripts/*.sh
