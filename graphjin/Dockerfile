# stage: 1
FROM node:lts-slim as react-build
WORKDIR /web
COPY /serv/web/ ./
RUN yarn
RUN yarn build

# stage: 2
FROM golang:1.21 as go-build
RUN go install github.com/rafaelsq/wtc@latest

WORKDIR /app
COPY . /app

RUN mkdir -p /app/serv/web/build
COPY --from=react-build /web/build/ ./serv/web/build

RUN go mod download
RUN make build

# stage: 3
FROM alpine:latest
WORKDIR /

RUN apk add --no-cache tzdata
RUN apk add --no-cache libc6-compat
RUN mkdir -p /config

COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=go-build /app/graphjin .

RUN chmod +x /graphjin

#USER nobody

ENV GO_ENV production

ENTRYPOINT ["./graphjin"]
