bold := $(shell tput bold)
sgr0 := $(shell tput sgr0)

test:
	@printf "$(bold)Running 'functions/retriever' tests$(sgr0)\n"
	@cd functions/retriever && go test *.go -v
	@printf "$(bold)Running 'functions/processor' tests$(sgr0)\n"
	@cd functions/processor && go test *.go -v
	@printf "$(bold)Running 'functions/notification' tests$(sgr0)\n"
	@cd functions/notification && go test *.go -v

lint:
	yamllint template.yaml
	sam validate
	@printf "$(bold)golangci-run 'functions/retriever'$(sgr0)\n"
	@cd functions/retriever && golangci-lint run
	@printf "$(bold)golangci-run 'functions/processor'$(sgr0)\n"
	@cd functions/processor && golangci-lint run
	@printf "$(bold)golangci-run 'functions/notification'$(sgr0)\n"
	@cd functions/notification && golangci-lint run
	shellcheck ./scripts/*.sh
