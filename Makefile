# Read version from build.xml file
VERSION ?= $(shell powershell -NoProfile -Command "([xml](Get-Content build.xml)).project.version")

DIST_DIR = dist/v$(VERSION)
GO_BUILD = go build -ldflags="-s -w -X morrow/cmd.Version=$(VERSION)"

WIN64_BIN = $(DIST_DIR)/morrow-v$(VERSION)_windows-x64.exe
WIN86_BIN = $(DIST_DIR)/morrow-v$(VERSION)_windows-x86.exe
WINARM_BIN = $(DIST_DIR)/morrow-v$(VERSION)_windows-arm64.exe
LINUX64_BIN = $(DIST_DIR)/morrow-v$(VERSION)_linux-amd64
LINUXARM_BIN = $(DIST_DIR)/morrow-v$(VERSION)_linux-arm64
LINUXARM32_BIN = $(DIST_DIR)/morrow-v$(VERSION)_linux-arm

.PHONY: all build-go build-msi verify clean help set-version

all: build-go build-msi verify

help:
	@echo "Usage:"
	@echo "  make build-go      Build Go binaries for all platforms"
	@echo "  make build-msi     Build WiX installers"
	@echo "  make all           Build everything"
	@echo "  make verify        Check every artifact in dist/ targets the right OS/arch"
	@echo "  make clean         Remove build artifacts"
	@echo "  make set-version   Update version. Usage: make set-version V=1.1.0"

set-version:
	powershell -Command "(Get-Content build.xml) -replace '<version>.*</version>', '<version>$(V)</version>' | Set-Content build.xml"
	@echo Version updated to $(V). Run 'make all' to rebuild.

# Each binary sets GOOS/GOARCH/GOARM as exported target-specific variables, so
# make passes them through the environment whichever shell runs the recipe
# (sh or cmd.exe). Do not set them inside a quoted "$$env:..." string: sh
# expands $env to nothing there and every target silently builds for the host.
export CGO_ENABLED = 0

$(WIN64_BIN): export GOOS = windows
$(WIN64_BIN): export GOARCH = amd64
$(WIN86_BIN): export GOOS = windows
$(WIN86_BIN): export GOARCH = 386
$(WINARM_BIN): export GOOS = windows
$(WINARM_BIN): export GOARCH = arm64
$(LINUX64_BIN): export GOOS = linux
$(LINUX64_BIN): export GOARCH = amd64
$(LINUXARM_BIN): export GOOS = linux
$(LINUXARM_BIN): export GOARCH = arm64
$(LINUXARM32_BIN): export GOOS = linux
$(LINUXARM32_BIN): export GOARCH = arm
$(LINUXARM32_BIN): export GOARM = 7

GO_BINS = $(WIN64_BIN) $(WIN86_BIN) $(WINARM_BIN) $(LINUX64_BIN) $(LINUXARM_BIN) $(LINUXARM32_BIN)

# The binaries are phony so they always rebuild, as before.
.PHONY: $(GO_BINS)

build-go: $(GO_BINS)
	go run ./script/verifydist $(DIST_DIR)

$(GO_BINS): | $(DIST_DIR)
	$(GO_BUILD) -o $@ main.go

$(DIST_DIR):
	@echo Building Go binaries...
	powershell -Command "if (!(Test-Path $(DIST_DIR))) { New-Item -ItemType Directory -Force -Path $(DIST_DIR) }"

build-msi: build-go
	@echo Building WiX installers...
	dotnet build installer/morrow.wixproj -p:Platform=x64 -p:Version=$(VERSION) -c Release -nr:false
	dotnet build installer/morrow.wixproj -p:Platform=x86 -p:Version=$(VERSION) -c Release -nr:false
	dotnet build installer/morrow.wixproj -p:Platform=arm64 -p:Version=$(VERSION) -c Release -nr:false
	@echo Copying MSIs...
	powershell -Command "Copy-Item dist/wix-build/bin/x64/Release/morrow-v$(VERSION)-x64.msi $(DIST_DIR)/morrow-v$(VERSION)_windows-x64.msi -Force"
	powershell -Command "Copy-Item dist/wix-build/bin/x86/Release/morrow-v$(VERSION)-x86.msi $(DIST_DIR)/morrow-v$(VERSION)_windows-x86.msi -Force"
	powershell -Command "Copy-Item dist/wix-build/bin/arm64/Release/morrow-v$(VERSION)-arm64.msi $(DIST_DIR)/morrow-v$(VERSION)_windows-arm64.msi -Force"
	@echo Cleaning up temporary WiX build files and raw Windows executables...
	powershell -Command "Remove-Item -Recurse -Force dist/wix-build -ErrorAction SilentlyContinue"
	powershell -Command "Remove-Item $(WIN64_BIN), $(WIN86_BIN), $(WINARM_BIN) -ErrorAction SilentlyContinue"

verify:
	go run ./script/verifydist $(DIST_DIR)


clean:
	@echo Cleaning build artifacts...
	powershell -Command "Remove-Item -Recurse -Force dist -ErrorAction SilentlyContinue"





