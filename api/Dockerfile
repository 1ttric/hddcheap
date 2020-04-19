FROM golang:alpine AS build
RUN apk update && \
    apk add --no-cache git

WORKDIR /app/src
COPY . .
RUN CGO_ENABLED=0 go build -v -trimpath -o /app/build

FROM selenium/standalone-chrome
COPY --from=build /app/build /app/build

ENTRYPOINT ["/app/build", "--verbosity", "trace"]