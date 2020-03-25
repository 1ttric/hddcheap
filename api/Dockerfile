FROM golang:alpine AS build
RUN apk update && \
    apk add --no-cache git

WORKDIR /app/src
COPY . .
RUN go get -d -v
RUN go build -v -o /app/build

FROM scratch
COPY --from=build /app/build /app/build

ENTRYPOINT ["/app/build"]