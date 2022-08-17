bold := $(shell tput bold)
sgr0 := $(shell tput sgr0)

build:
	sam build

test:
	@printf "$(bold)Running 'functions/retriever' tests$(sgr0)\n"
	@cd functions/retriever && go test *.go -v
	@printf "$(bold)Running 'functions/worker' tests$(sgr0)\n"
	@cd functions/worker && go test *.go -v
	@printf "$(bold)Running 'functions/notification' tests$(sgr0)\n"
	@cd functions/notification && go test *.go -v

lint:
	yamllint template.yaml
	# sam validate
	@printf "$(bold)golangci-run 'functions/retriever'$(sgr0)\n"
	@cd functions/retriever && golangci-lint run
	@printf "$(bold)golangci-run 'functions/worker'$(sgr0)\n"
	@cd functions/worker && golangci-lint run
	@printf "$(bold)golangci-run 'functions/notification'$(sgr0)\n"
	@cd functions/notification && golangci-lint run
	shellcheck ./scripts/*.sh
