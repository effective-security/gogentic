include .project/gomod-project.mk
export GO111MODULE=on
BUILD_FLAGS=
export COVERAGE_EXCLUSIONS="vendor|tests|third_party|api/pb/|main\.go|testsuite\.go|gomock|mocks/|\.gen\.go|\.pb\.go"

export OPENAI_API_KEY=fakekey
export TAVILY_API_KEY=fakekey
export ANTHROPIC_API_KEY=fakekey
export PERPLEXITY_TOKEN=fakekey
export GOOGLEAI_TOKEN=fakekey
export AZURE_OPENAI_API_KEY=fakekey
export AZURE_OPENAI_URL=fakeurl

.PHONY: *

.SILENT:

default: help

all: clean tools generate change_log covtest

#
# clean produced files
#
clean:
	go clean ./...
	rm -rf \
		${COVPATH} \
		${PROJ_BIN}

tools:
	go install github.com/effective-security/cov-report/cmd/cov-report@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install go.uber.org/mock/mockgen@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0
	go install github.com/effective-security/golangci-linters/cmd/custom-linters@latest

change_log:
	echo "Recent changes" > ./change_log.txt
	echo "Build Version: $(GIT_VERSION)" >> ./change_log.txt
	echo "Commit: $(GIT_HASH)" >> ./change_log.txt
	echo "==================================" >> ./change_log.txt
	git log -n 20 --pretty=oneline --abbrev-commit >> ./change_log.txt

