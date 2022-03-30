# stage: 1
FROM node:lts-slim as react-build
WORKDIR /web
COPY /serv/web/ ./
RUN yarn
RUN yarn build

# stage: 2
FROM golang:1.18 as go-build
RUN go install github.com/rafaelsq/wtc@latest

WORKDIR /app
COPY . /app

RUN mkdir -p /app/serv/web/build
COPY --from=react-build /web/build/ ./serv/web/build

RUN go mod download
RUN make build

# stage: 3
FROM gcr.io/distroless/static
WORKDIR /

COPY --from=go-build /app/graphjin .
ENV GO_ENV production

ENTRYPOINT ["./graphjin"]
CMD ["serv"]
