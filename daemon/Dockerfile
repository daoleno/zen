FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o zen-daemon ./cmd/zen-daemon/

FROM alpine:3.21
RUN apk add --no-cache tmux
COPY --from=builder /build/zen-daemon /usr/local/bin/zen-daemon
EXPOSE 9876
ENTRYPOINT ["zen-daemon"]
