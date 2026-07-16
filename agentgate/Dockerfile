# Two-stage build: compile in a full Go image, ship only the static binary.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /agentgate ./cmd/agentgate

FROM alpine:3.21
COPY --from=build /agentgate /agentgate
EXPOSE 9000
ENTRYPOINT ["/agentgate"]
