FROM golang:1.20-alpine as buildbase


RUN apk add git build-base ca-certificates
WORKDIR /go/src/github.com/artemskriabin/go-jsonrpc-proxy
COPY . .

RUN CGO_ENABLED=1 GO111MODULE=on GOOS=linux go build  -o /usr/local/bin/go-jsonrpc-proxy /go/src/github.com/artemskriabin/go-jsonrpc-proxy/cmd/main.go

FROM scratch
COPY --from=alpine:3.9 /bin/sh /bin/sh
COPY --from=alpine:3.9 /usr /usr
COPY --from=alpine:3.9 /lib /lib

COPY --from=buildbase /usr/local/bin/go-jsonrpc-proxy  /usr/local/bin/go-jsonrpc-proxy 
COPY --from=buildbase /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENTRYPOINT ["go-jsonrpc-proxy"]