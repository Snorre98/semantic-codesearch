FROM golang:1.25-alpine AS builder

RUN apk add --no-cache build-base

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -ldflags '-linkmode external -extldflags "-static"' -o /mcp-code-search .

FROM alpine:3.21
COPY --from=builder /mcp-code-search /mcp-code-search
ENTRYPOINT ["/mcp-code-search"]
