FROM golang:latest

WORKDIR /go
COPY . .

run export GOBIN=/go/bin/ && \
    go get github.com/sfloresk/tviewer && \
    go install github.com/sfloresk/tviewer

EXPOSE 9090

WORKDIR /go/

CMD ["/go/bin/tviewer"]
