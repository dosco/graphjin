# stage: 1
FROM node:10 as react-build
WORKDIR /web
COPY web/ ./
RUN yarn
RUN yarn build

# stage: 2
FROM golang:1.13.4-alpine as go-build
RUN apk update && \
    apk add --no-cache make && \
    apk add --no-cache git && \
    apk add --no-cache upx=3.95-r2

RUN GO111MODULE=off go get -u github.com/rafaelsq/wtc

WORKDIR /app
COPY . /app

RUN mkdir -p /app/web/build
COPY --from=react-build /web/build/ ./web/build/

RUN go mod vendor
RUN make build
RUN echo "Compressing binary, will take a bit of time..." && \
  upx --ultra-brute -qq super-graph && \
  upx -t super-graph

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
