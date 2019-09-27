# stage: 1
FROM node:10 as react-build
WORKDIR /web
COPY web/ ./
RUN yarn
RUN yarn build

# stage: 2
FROM golang:1.13beta1-alpine as go-build
RUN apk update && \
    apk add --no-cache git && \
    apk add --no-cache upx=3.95-r2

RUN go get -u github.com/dosco/esc && \
    go get -u github.com/shanzi/wu && \
    go install github.com/shanzi/wu && \
    go get github.com/GeertJohan/go.rice/rice

WORKDIR /app
COPY . /app

RUN mkdir -p /app/web/build
COPY --from=react-build /web/build/ ./web/build/

ENV GO111MODULE=on
RUN go mod vendor
# RUN go generate ./... && \
#   CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o super-graph && \
#   upx --ultra-brute -qq super-graph && \
#   upx -t super-graph

RUN go generate ./... && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o super-graph

# stage: 3
FROM alpine:latest
WORKDIR /

RUN apk add --no-cache tzdata
RUN mkdir -p /config

COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=go-build /app/config/* /config/
COPY --from=go-build /app/super-graph .

RUN chmod +x /super-graph
USER nobody

EXPOSE 8080

CMD ./super-graph serv
