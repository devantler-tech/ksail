ROOT=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
CACHE_DIR=$(ROOT)/../assets

non-mach-o:
	GOOS=linux GOARCH=amd64 go build -o linux_amd64 main.go
	mv linux_amd64 $(CACHE_DIR)