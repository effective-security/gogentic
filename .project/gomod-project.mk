# gomod-project.mk: this contains commonly used helpers for makefiles.
SHELL=/bin/bash

# Used envaronment variables:
#
# 	PROJ_DIR
#		project's absolute root directory
#
# 	PROJ_BIN
#		project's bin folder
#
# 	ORG_NAME
#		Git organization name, for example: github.com/go-phorce
#
#	PROJ_NAME
#		Git project name, for example: go-makefile
#
#	REPO_NAME
#		Git repo name consists of the org and project: github.com/go-phorce/go-makefile
#
#	PROJ_GOFILES
#		List of all .go files in the project, exluding vendor and tools
#
# Test flags:
#
#	TEST_RACEFLAG
#		Use -race when running go test
#
#	TEST_GORACEOPTIONS
#		Race options
#
# Functions:
#
#	show_dep_updates {folder}
#		Show dependencies updates in {folder}
#
#	httpsclone {org} {repo} {destination_dir}
#
#	go_test_cover
#
#	go_test_cover_junit


PROJ_ROOT := $(shell pwd)

## Project variables
ORG_NAME := $(shell .project/config_var.sh project_org)
PROJ_NAME := $(shell .project/config_var.sh project_name)
REPO_NAME := ${ORG_NAME}/${PROJ_NAME}
PROJ_PACKAGE := ${REPO_NAME}

## Common variables
HOSTNAME := $(shell echo $$HOSTNAME)
UNAME := $(shell uname)
GITHUB_HOST := github.com
GOLANG_HOST := golang.org
# GIT_DIRTY is empty if the project is not modified, otherwise it's current host name
GIT_DIRTY := $(shell git describe --dirty --always --tags --long | grep -q -e '-dirty' && echo -$$HOSTNAME)
GIT_HASH := $(shell git rev-parse --short HEAD)
# number of commits
COMMITS_COUNT := $(shell git rev-list --count ${GIT_HASH})
#
PROD_VERSION := $(shell cat .VERSION)
GIT_VERSION := $(shell printf %s.%d%s ${PROD_VERSION} ${COMMITS_COUNT} ${GIT_DIRTY})
COVPATH=.coverage

# List of all .go files in the project, excluding vendor and .tools
GOFILES_NOVENDOR = $(shell find . -type f -name '*.go' -not -path "./vendor/*" -not -path "./.tools/*" -not -path "./.gopath/*")
GOPACKAGES = $(shell go list ./...)
GOPACKAGES_FOR_TEST = $(shell go list ./... | grep -v /tests/)

export PROJ_DIR=$(PROJ_ROOT)
export PROJ_BIN=$(PROJ_ROOT)/bin
export GOBIN=$(PROJ_ROOT)/bin
export PATH := ${PATH}:${PROJ_BIN}

# List of all .go files in the project, exluding vendor and tools
PROJ_GOFILES = $(shell find . -type f -name '*.go' -not -path "./vendor/*" -not -path "./.gopath/*" -not -path "./.tools/*")

COVERAGE_EXCLUSIONS="/rt\.go|/bindata\.go|_test\.go|_mock\.go|main\.go"

# flags
INTEGRATION_TAG="integration"
TEST_RACEFLAG ?=
TEST_GORACEOPTIONS ?=

# flag to enable golang race detector. Usage: make $(test_target) RACE=true. For example, make test RACE=true
RACE ?=
ifeq ($(RACE),true)
	TEST_GORACEOPTIONS = "log_path=${PROJ_DIR}/${COVPATH}/race/report"
	TEST_RACEFLAG = -race
endif

## Common targets/functions for golang projects
# 	They assume that
#	a) GOPATH has been set with an export GOPATH somewhere
#	b) the Makefile variable PROJ_PACKAGE has been set to the name of the go pacakge to operate on
#

# go_test_cover will run go test on a package tree, with code coverage turned on, it writes coverage results
# to ./${COVPATH}
# the 5 params are
#		1) the working dir to run the tests in
#		2) the flags to run the tests with
#		3) flag to enable race detector
#		4) options to race detector such as log_path for storing the results of the race detector
#		5) the name of the PROJ_DIR package to test
#		6) the list of source exclusions to apply to the generated code coverage result calculation
#
# it assumes you've built the cov-report tool
define go_test_cover
	echo  "Testing in $(1)"
	rm -rf ${COVPATH}
	mkdir -p ${COVPATH}/race
	exitCode=0 \
	&& cd $(1) && go list $(5)/... | grep -v /tests/ | ( while read -r pkg; do \
		result=`GORACE=$(4) go test -p 40 $(2) $$pkg -coverpkg=$(5)/... -covermode=count $(3) \
			-coverprofile=${COVPATH}/cc_$$(echo $$pkg | tr "/" "_").out \
			2>&1 | grep --invert-match "warning: no packages"` \
			&& test_result=`echo "$$result" | tail -1` \
			&& echo "$$test_result" \
			&& if echo $$test_result | grep ^FAIL ; then \
				exitCode=1 && echo "Test for $$pkg failed. Result: $$result, exit code: $$exitCode" \
			; fi \
		; done \
		&& echo "Completed with status code $$exitCode" \
		&& if [ $$exitCode -ne "0" ] ; then echo "Test failed, exit code: $$exitCode" && exit $$exitCode ; fi )
	cov-report -ex $(6) -cc ${COVPATH}/combined.out ${COVPATH}/cc*.out
	cp ${COVPATH}/combined.out ${PROJ_DIR}/coverage.out
endef

# same as go_test_cover except it also generates results in the junit format
# assuming ${TOOLS_BIN} contains go-junit-report & cov-report
define go_test_cover_junit
	echo  "Testing in $(1)"
	rm -rf ${COVPATH}
	mkdir -p ${COVPATH}/race
	set -o pipefail; failure=0; while read -r pkg; do \
		cd $(1) && GORACE=$(4) go test $(2) -v $$pkg -coverpkg=$(5)/... -covermode=count $(3) \
			-coverprofile=${COVPATH}/cc_$$(echo $$pkg | tr "/" "_").out \
			>> ${COVPATH}/citest_$$(echo $(5) | tr "/" "_").log \
			|| failure=1; \
    done <<< "$$(cd $(1) && go list $(5)/...)" && \
    cat ${COVPATH}/citest_$$(echo $(5) | tr "/" "_").log | go-junit-report >> ${COVPATH}/citest_$$(echo $(5) | tr "/" "_").xml && \
    exit $$failure
endef

# list the make targets
# http://stackoverflow.com/questions/4219255/how-do-you-get-the-list-of-targets-in-a-makefile/15058900#15058900
no_targets__:
list:
	sh -c "$(MAKE) -p no_targets__ | awk -F':' '/^[a-zA-Z0-9][^\$$#\/\\t=]*:([^=]|$$)/ {split(\$$1,A,/ /);for(i in A)print A[i]}' | grep -v '__\$$' | sort"

#
# print environment variables
#
vars:
	echo "PATH=$(PATH)"
	echo "PROJ_DIR=$(PROJ_DIR)"
	echo "PROJ_REPO_TARGET=$(PROJ_REPO_TARGET)"
	echo "GOROOT=$(GOROOT)"
	echo "GOBIN=$(GOBIN)"
	echo "GOPATH=$(GOPATH)"
	echo "PROJ_PACKAGE=$(PROJ_PACKAGE)"
	echo "TOOLS_PATH=$(TOOLS_PATH)"
	echo "GIT_VERSION=$(GIT_VERSION)"
	go version

#
# list packages
#
lspkg:
	go list ./...

#
# print out GO environment
#
env:
	go env

#
# GO test with bench
#
bench:
	go test  ${TEST_RACEFLAG} -bench . ${PROJ_PACKAGE}/...

generate:
	go generate ./...
	gofmt -s -l -w -r 'interface{} -> any' .

fmt:
	echo "Running Fmt"
	gofmt -s -l -w -r 'interface{} -> any' .

fmt-check:
	echo "Running Fmt check"
	gofmt -d -l -r 'interface{} -> any' .
	@test -z "$(shell gofmt -l -r 'interface{} -> any' . | tee /dev/stderr)"

vet:
	echo "Running vet"
	go vet ${BUILD_FLAGS} ${PROJ_PACKAGE}/...

vulns:
	echo "Running vulns"
	govulncheck ${PROJ_PACKAGE}/...

lint: fmt vet
	echo "Running lint"
	golangci-lint run --timeout 20m0s ./...

test:
	echo "Running test"
	go test ${TEST_FLAGS} ${TEST_RACEFLAG} ${GOPACKAGES_FOR_TEST}

testshort:
	echo "Running testshort"
	go test ${TEST_FLAGS} ${TEST_RACEFLAG} ${GOPACKAGES_FOR_TEST} --test.short

# you can run a subset of tests with make sometests testname=<testnameRegex>
sometests:
	go test ${TEST_FLAGS} ${TEST_RACEFLAG} ${GOPACKAGES_FOR_TEST} --test.short -run $(testname)

covtest: fmt vet
	echo "Running covtest"
	$(call go_test_cover,${PROJ_DIR},${BUILD_FLAGS},${TEST_RACEFLAG},${TEST_GORACEOPTIONS},.,${COVERAGE_EXCLUSIONS})

# Runs integration tests as well
testint: fmt vet lint
	echo "Running testint"
	go test ${TEST_RACEFLAG} -tags=${INTEGRATION_TAG} ${GOPACKAGES_FOR_TEST}

# shows the coverages results assuming they were already generated by a call to go_test_cover
coverage:
	echo "Running coverage"
	go tool cover -html="${COVPATH}/combined.out"

# generates a HTML based code coverage report, and writes it to a file in the results directory
# assumes you've run go_test_cover (or go_test_cover_junit)
cicoverage:
	echo "Running cicoverage"
	mkdir -p ${COVPATH}/cover
	go tool cover -html="${COVPATH}/combined.out" -o "${COVPATH}/cover/coverage.html"

# as Jenkins runs citestint as well which will run all unit tests + integration tests with code coverage
# this unitest step can skip coverage reporting which speeds it up massively
citest: vet lint
	echo "Running citest"
	$(call go_test_cover_junit,${PROJ_DIR},${BUILD_FLAGS},${TEST_RACEFLAG},${TEST_GORACEOPTIONS},.,${COVERAGE_EXCLUSIONS})
	cov-report -fmt xml -o ${COVPATH}/coverage.xml -ex ${COVERAGE_EXCLUSIONS} -cc ${COVPATH}/combined.out ${COVPATH}/cc*.out
	cov-report -fmt ds -o ${COVPATH}/summary.xml -ex ${COVERAGE_EXCLUSIONS} ${COVPATH}/cc*.out

coveralls:
	echo "Running coveralls"
	goveralls -v -coverprofile=coverage.out -service=travis-ci -package ./...

help:
	echo "make vars - print make variables"
	echo "make env - pring GO environment"
	echo "make lspkg - list GO packeges in the current project"
	echo "make generate - generate GO files"
	echo "make bench - GO test with bench"
	echo "make fmt - run go fmt on project files"
	echo "make vet - run go vet on project files"
	echo "make lint - run go lint on project files"
	echo "make test - run test"
	echo "make testshort - run test with -short flag"
	echo "make covtest - run test with coverage report"
	echo "make coverage - open coverage report"
	echo "make coveralls - publish coverage to coveralls"
