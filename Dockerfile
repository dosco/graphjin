# stage: 1
FROM node:10 as react-build
WORKDIR /web
COPY /internal/serv/web/ ./
RUN yarn
RUN yarn build



# stage: 2
FROM golang:1.14-alpine as go-build
RUN apk update && \
    apk add --no-cache make && \
    apk add --no-cache git && \
    apk add --no-cache jq && \
    apk add --no-cache upx=3.95-r2

RUN GO111MODULE=off go get -u github.com/rafaelsq/wtc

ARG SOPS_VERSION=3.5.0
ADD https://github.com/mozilla/sops/releases/download/v${SOPS_VERSION}/sops-v${SOPS_VERSION}.linux /usr/local/bin/sops
RUN chmod 755 /usr/local/bin/sops

WORKDIR /app
COPY . /app

RUN mkdir -p /app/internal/serv/web/build
COPY --from=react-build /web/build/ ./internal/serv/web/build

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
COPY --from=go-build /app/internal/scripts/start.sh .
COPY --from=go-build /usr/local/bin/sops .

RUN chmod +x /super-graph
RUN chmod +x /start.sh

USER nobody

ENV GO_ENV production

ENTRYPOINT ["./start.sh"]
CMD ["./super-graph", "serv"]
