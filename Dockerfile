FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bt-agent .
FROM alpine:latest
COPY --from=builder /bt-agent /usr/local/bin/bt-agent
VOLUME /data   
EXPOSE 2030
ENTRYPOINT ["bt-agent"]