FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/vidpipe-api .

# ---

FROM alpine:3.19

# ffmpeg provides ffprobe, used by the upload handler to extract metadata
# (duration, resolution, codec).
RUN apk add --no-cache ca-certificates tzdata ffmpeg

WORKDIR /app

COPY --from=builder /app/vidpipe-api .

EXPOSE 8080

ENTRYPOINT ["./vidpipe-api"]
