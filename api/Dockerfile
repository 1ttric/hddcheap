FROM golang:alpine AS build
RUN apk update && \
    apk add --no-cache git

WORKDIR /app/src
COPY . .
RUN go get -d -v ./...
RUN CGO_ENABLED=0 go build -v -trimpath -o /app/build

FROM chromedp/headless-shell
COPY --from=build /app/build /app/build

EXPOSE 3001
ENTRYPOINT ["/app/build"]