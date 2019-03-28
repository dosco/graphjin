# stage: 1
FROM node:10 as react-build
WORKDIR /web
COPY web/ ./
RUN yarn
RUN yarn build

# stage: 2
FROM golang:1.12-alpine as go-build
RUN apk update && \
    apk add --no-cache git && \
    apk add --no-cache upx=3.95-r1

RUN go get -u github.com/dosco/esc && \
    go get -u github.com/pilu/fresh

WORKDIR /app
COPY . /app

RUN mkdir -p /app/web/build
COPY --from=react-build /web/build/ ./web/build/

ENV GO111MODULE=on
RUN go mod vendor
RUN go generate ./... && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o service && \
    upx --ultra-brute -qq service && \
    upx -t service

# stage: 3
FROM alpine:latest
WORKDIR /app

RUN apk add --no-cache tzdata

COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=go-build /app/service .
COPY --from=go-build /app/*.yml ./

RUN chmod +x /app/service
USER nobody

EXPOSE 8080

CMD ./service
