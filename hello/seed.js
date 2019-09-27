version: '3'
services:
  db:
    image: postgres
    ports:
      - "5432:5432"

  hello_api:
    image: dosco/super-graph:latest
    environment:
      GO_ENV: "development"
    volumes:
     - ./config:/config
    ports:
      - "8080:8080"
    depends_on:
      - db