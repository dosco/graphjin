FROM golang:1.12-alpine as builder
RUN apk update && \
    apk add --no-cache git && \
    apk add --no-cache upx=3.95-r1

RUN go get -u github.com/dosco/esc && \
    go get -u github.com/pilu/fresh

WORKDIR /app
ADD . /app

ENV GO111MODULE=on
RUN go mod vendor
RUN go generate ./... && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o service && \
    upx --ultra-brute -qq service && \
    upx -t service

#second stage
FROM alpine:latest
WORKDIR /app

RUN apk add --no-cache tzdata

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/service .
COPY --from=builder /app/*.yml ./

RUN chmod +x /app/service
USER nobody

EXPOSE 8080

CMD ./service
