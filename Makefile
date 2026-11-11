.PHONY: test
ifeq ($(shell go env GOOS),windows)
EXE=.exe
else
EXE=
endif

DIST=dist
BINDIR=.

BASENAME=$(notdir $(shell pwd))
PROGRAM=$(BINDIR)/$(BASENAME)$(EXE)
LAST_RELEASE=

REPO=$(shell go list | head -n 1)
IMAGE=$(BASENAME)
VERSION ?= $(shell ./utils/version.sh)
GO_VERSION ?= $(shell grep '^go ' go.mod | awk '{print $$2}')
DOCKER=docker
PACKAGE=$(DIST)/$(basename $(notdir $(PROGRAM)))-$(shell go env GOOS)-$(shell go env GOARCH).zip


.PHONY: $(PROGRAM)

all: $(PROGRAM)

compile: $(PROGRAM)

$(PROGRAM): $(BINDIR)
	mkdir -p $(dir $@)
	go build -ldflags="-X '$(REPO)/program.Version=${VERSION}'" -o $(PROGRAM)

package: $(PACKAGE)

$(PACKAGE): $(PROGRAM)

# These next 2 recipes know how to make .zip and .tar files, which are used implicitly in making the package
%.zip:
	mkdir $(dir $@)
	zip -j $@ $?

%.tar.gz %.tgz:
	mkdir $(dir $@)
	tar -czf $@ -C $(dir $<) $(notdir $<)



install:
	go install -ldflags="-X '$(REPO)/program.Version=${VERSION}'"


image:
	$(DOCKER) build --build-arg GO_VERSION=$(GO_VERSION) --build-arg PROGRAM=$(BASENAME) --build-arg VERSION=$(VERSION) -t $(IMAGE) .

test:
	go test -v ./...

vet:
	go vet ./...

# Browser scripts: TypeScript in web/src, compiled to the embedded assets in
# internal/target/html/assets (committed, since go install can't run tsc).
# npm and node only ever run in a container: the same pinned image as the
# tools Dockerfile, as your user, with the network on only for npm ci.
NODE_IMAGE ?= $(shell awk '/^FROM node:/ {print $$2; exit}' internal/tools/Dockerfile)
WEB_RUN = $(DOCKER) run --rm --user $(shell id -u):$(shell id -g) -e HOME=/tmp -v "$(CURDIR)":/src -w /src/web
WEB_OFFLINE = $(WEB_RUN) --network none $(NODE_IMAGE)

.PHONY: web web-test web-check
web/node_modules/.package-lock.json: web/package.json web/package-lock.json
	$(WEB_RUN) $(NODE_IMAGE) npm ci

web: web/node_modules/.package-lock.json ## compile web/src/*.ts into the embedded assets
	$(WEB_OFFLINE) npx tsc -p tsconfig.json

web-test: web ## type-check and run the browser-script tests
	$(WEB_OFFLINE) sh -c 'npx tsc -p tsconfig.test.json && node --test test/*.test.ts'

web-check: web-test ## fail if the committed assets don't match web/src
	git diff --exit-code -- internal/target/html/assets/
	@! git status --porcelain --untracked-files=all -- internal/target/html/assets/ | grep '^??' || { echo "untracked compiled assets: commit them"; exit 1; }

changelog: CHANGELOG.md
CHANGELOG.md: .chglog/config.yml
	git chglog $(LAST_RELEASE) >$@

.chglog/config.yml: go.mod
	sed -i.bak -e "s|repository_url:.*|repository_url: https://$(REPO)|" $@

hooks: .git/hooks/pre-commit

.git/hooks/pre-commit: .pre-commit-config.yaml
	pre-commit install
	pre-commit install --hook-type commit-msg


info::
	@echo BASENAME=$(BASENAME)
	@echo PROGRAM=$(PROGRAM)
	@echo IMAGE=$(IMAGE)


update-repo:
	./utils/update-repo.sh $(NEWREPO)

tools:
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install github.com/go-critic/go-critic/cmd/gocritic@latest
	go install github.com/securego/gosec/v2/cmd/gosec@latest
