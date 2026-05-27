FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o zen ./cmd/zen/

FROM alpine:3.21
RUN apk add --no-cache tmux
COPY --from=builder /build/zen /usr/local/bin/zen
EXPOSE 9876
ENTRYPOINT ["zen"]
