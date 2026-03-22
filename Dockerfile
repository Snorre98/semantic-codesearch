FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /mcp-code-search .

FROM alpine:3.21
COPY --from=builder /mcp-code-search /mcp-code-search
ENTRYPOINT ["/mcp-code-search"]
