# stage: 1
FROM node:latest as react-build
WORKDIR /web
COPY /serv/web/ ./
RUN yarn
RUN yarn build



# stage: 2
FROM golang:1.17-buster as go-build
RUN apt-get -y update
RUN apt-get -y upgrade
RUN apt-get -y install build-essential git-all jq 

RUN GO111MODULE=off go get -u github.com/rafaelsq/wtc

ARG SOPS_VERSION=3.5.0
ADD https://github.com/mozilla/sops/releases/download/v${SOPS_VERSION}/sops-v${SOPS_VERSION}.linux /usr/local/bin/sops
RUN chmod 755 /usr/local/bin/sops

WORKDIR /app
COPY . /app

RUN mkdir -p /app/serv/web/build
COPY --from=react-build /web/build/ ./serv/web/build

RUN go mod vendor
RUN make build
# RUN echo "Compressing binary, will take a bit of time..." && \
#   upx --ultra-brute -qq graphjin && \
#   upx -t graphjin



# stage: 3
FROM alpine:latest
WORKDIR /

RUN apk add --no-cache tzdata
RUN apk add --no-cache libc6-compat
RUN mkdir -p /config

COPY --from=go-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=go-build /app/graphjin .
COPY --from=go-build /app/internal/scripts/start.sh .
COPY --from=go-build /usr/local/bin/sops .

RUN chmod +x /graphjin
RUN chmod +x /start.sh

#USER nobody

ENV GO_ENV production

ENTRYPOINT ["./start.sh"]
CMD ["serv"]
