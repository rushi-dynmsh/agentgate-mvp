FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /toy-server .

FROM alpine:3.21
COPY --from=build /toy-server /toy-server
EXPOSE 8080
ENTRYPOINT ["/toy-server"]
