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


image: .Dockerfile.tmp
	$(DOCKER) build -f $< --build-arg GO_VERSION=$(GO_VERSION) --build-arg PROGRAM=$(BASENAME) --build-arg VERSION=$(VERSION) --build-arg BASENAME=$(BASENAME) -t $(IMAGE) .

.Dockerfile.tmp: Dockerfile
	sed -e "s|^ENTRYPOINT.*|ENTRYPOINT [\"/${BASENAME}\"]|" < $< > $@.tmp
	mv -f $@.tmp $@

test:
	go test -v ./...

vet:
	go vet ./...

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

# --- Publishing (Cloudflare Pages). Settings usually live in publish.local.mk:
#   PUBLISH_CONFIG, PAGES_PROJECT, CLOUDFLARE_API_TOKEN_REF, CLOUDFLARE_ACCOUNT_ID[_REF]
-include publish.local.mk
PUBLISH_CONFIG ?= publish.yaml
PUBLISH_OUTPUT ?= public
PAGES_BRANCH ?= main
export CLOUDFLARE_API_TOKEN_REF CLOUDFLARE_ACCOUNT_ID CLOUDFLARE_ACCOUNT_ID_REF

.PHONY: publish-tools site publish
publish-tools:
	$(DOCKER) build -q --network host -t obsidian-publish-tools tools/publish

site: $(PROGRAM) publish-tools
	PATH="$(CURDIR)/tools/publish:$$PATH" $(PROGRAM) build -c $(PUBLISH_CONFIG) -o $(PUBLISH_OUTPUT) --strict

publish: site
	@test -n "$(PAGES_PROJECT)" || { echo "set PAGES_PROJECT" >&2; exit 2; }
	SITE=$(PUBLISH_OUTPUT) tools/publish/wrangler pages deploy /site --project-name $(PAGES_PROJECT) --branch $(PAGES_BRANCH) --commit-dirty=true
