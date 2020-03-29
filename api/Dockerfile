FROM golang:alpine AS build
RUN apk update && \
    apk add --no-cache git

WORKDIR /app/src
COPY . .
RUN go get -d -v
RUN CGO_ENABLED=0 go build -v -o /app/build

FROM selenium/standalone-chrome
COPY --from=build /app/build /app/build

ENTRYPOINT ["/app/build"]