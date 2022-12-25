# Attain base image
FROM golang:latest AS build-env

# Setup govendor
WORKDIR /go/src/kvstore
# copy files
COPY ../ .

WORKDIR /go/src/kvstore/replicatedKVStore

# Compile the application
RUN GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o kvstore .

EXPOSE 80
EXPOSE 8080

CMD ["./kvstore"]