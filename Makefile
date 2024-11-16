VERSION = "1.1.0"

dev:
	find -type f \( -name "*.go" \) | entr -r go run *.go -u

build: clean-build amd64 arm
	@echo "Building amd64 and arm version"

clean-build:
	- rm release -Rf

amd64:
	mkdir -p release/linux-amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-X 'main.ldVersion=$(VERSION)'" -o release/linux-amd64/journalctl-proxy -v *.go

arm:
	mkdir -p release/linux-arm
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 go build -ldflags="-X 'main.ldVersion=$(VERSION)'"  -o release/linux-arm/journalctl-proxy -v *.go
