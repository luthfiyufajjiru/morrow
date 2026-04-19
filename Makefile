# Read version from build.xml file
VERSION ?= $(shell powershell -NoProfile -Command "([xml](Get-Content build.xml)).project.version")

DIST_DIR = dist/v$(VERSION)
GO_BUILD = go build -ldflags='-s -w -X morrow/cmd.Version=$(VERSION)'

WIN64_BIN = $(DIST_DIR)/morrow-v$(VERSION)_windows-x64.exe
WIN86_BIN = $(DIST_DIR)/morrow-v$(VERSION)_windows-x86.exe
WINARM_BIN = $(DIST_DIR)/morrow-v$(VERSION)_windows-arm64.exe
LINUX64_BIN = $(DIST_DIR)/morrow-v$(VERSION)_linux-amd64
LINUXARM_BIN = $(DIST_DIR)/morrow-v$(VERSION)_linux-arm64

.PHONY: all build-go build-msi clean help set-version

all: build-go build-msi

help:
	@echo "Usage:"
	@echo "  make build-go      Build Go binaries for all platforms"
	@echo "  make build-msi     Build WiX installers"
	@echo "  make all           Build everything"
	@echo "  make clean         Remove build artifacts"
	@echo "  make set-version   Update version. Usage: make set-version V=1.1.0"

set-version:
	powershell -Command "(Get-Content build.xml) -replace '<version>.*</version>', '<version>$(V)</version>' | Set-Content build.xml"
	@echo Version updated to $(V). Run 'make all' to rebuild.

build-go:
	@echo Building Go binaries...
	powershell -Command "if (!(Test-Path $(DIST_DIR))) { New-Item -ItemType Directory -Force -Path $(DIST_DIR) }"
	powershell -Command "$$env:GOOS='windows'; $$env:GOARCH='amd64'; $(GO_BUILD) -o $(WIN64_BIN) main.go"
	powershell -Command "$$env:GOOS='windows'; $$env:GOARCH='386'; $(GO_BUILD) -o $(WIN86_BIN) main.go"
	powershell -Command "$$env:GOOS='windows'; $$env:GOARCH='arm64'; $(GO_BUILD) -o $(WINARM_BIN) main.go"
	powershell -Command "$$env:GOOS='linux'; $$env:GOARCH='amd64'; $(GO_BUILD) -o $(LINUX64_BIN) main.go"
	powershell -Command "$$env:GOOS='linux'; $$env:GOARCH='arm64'; $(GO_BUILD) -o $(LINUXARM_BIN) main.go"

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


clean:
	@echo Cleaning build artifacts...
	powershell -Command "Remove-Item -Recurse -Force dist -ErrorAction SilentlyContinue"





