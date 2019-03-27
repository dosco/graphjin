FROM golang:1.12-alpine as builder
RUN apk update && \
    apk add --no-cache git && \
    apk add --no-cache upx=3.95-r1

COPY . /app
WORKDIR /app

ENV GO111MODULE=on
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o service && \
    upx --ultra-brute -qq service && \
    upx -t service
    
RUN go get github.com/pilu/fresh

#second stage
FROM alpine:latest
WORKDIR /app
RUN apk add --no-cache tzdata && \
    mkdir -p /app/web/build

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/service .
COPY web/build /app/web/build/
COPY dev.yml .

RUN chmod +x /app/service
USER nobody

EXPOSE 8080

CMD ./service
