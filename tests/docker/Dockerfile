FROM golang:1.26.1-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN CGO_ENABLED=0 go build -o /out/smallctl .

FROM alpine:3.22

RUN apk add --no-cache bash

COPY --from=build /out/smallctl /usr/local/bin/smallctl
COPY tests/docker/config.yaml /etc/smallctl/config.yaml
COPY tests/docker/run.sh /usr/local/bin/smallctl-integration-test

RUN chmod +x /usr/local/bin/smallctl-integration-test

ENV XDG_RUNTIME_DIR=/tmp/smallctl-runtime

ENTRYPOINT ["/usr/local/bin/smallctl-integration-test"]
