FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /transcode-worker .

FROM alpine:3.19

RUN apk add --no-cache ffmpeg ca-certificates

COPY --from=builder /transcode-worker /usr/local/bin/transcode-worker

ENTRYPOINT ["transcode-worker"]
