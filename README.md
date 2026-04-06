# vidpipe

the video processing backend that YouTube, Udemy, and Vimeo all had to build from scratch - except you get it with `docker compose up`.

upload a video. vidpipe transcodes it to adaptive streaming, generates captions with AI, and picks the best thumbnail. three workers, running in parallel, fully self-hosted. no API keys, no cloud bills, no vendor lock-in.

## why does this exist

every platform that handles video uploads ends up building the same pipeline:

```
raw upload -> transcode -> captions -> thumbnails -> serve
```

YouTube does this. Twitch does this. Every course platform, every internal training tool, every video-based product does this. but building it from scratch takes months - you need queue-based job distribution, S3-compatible storage, adaptive streaming, speech-to-text, frame analysis, and a dashboard to monitor it all.

vidpipe is that entire pipeline in one repo. clone it, run it, upload a video.

## how it works (phase by phase)

the pipeline has 4 phases. here's what happens when you upload a video:

### phase 1: upload

user uploads a video through the dashboard or API. the Go server extracts metadata (duration, resolution, codec) using ffprobe, stores the raw file in MinIO (S3-compatible storage), and publishes 3 jobs to Redis Streams.

```mermaid
flowchart TD
    A[User uploads video] --> B[Go API receives file]
    B --> C[ffprobe: extract duration, resolution, codec]
    C --> D[Store raw file in MinIO]
    C --> E[Publish 3 jobs to Redis Streams]
    E --> F[transcode job]
    E --> G[caption job]
    E --> H[thumbnail job]
```

at this point the API responds immediately with the video ID and metadata. the user doesn't wait for processing.

### phase 2: parallel processing

three independent workers pick up their jobs from Redis and process the video simultaneously. each worker is a separate Docker container that can scale independently.

```mermaid
flowchart TD
    subgraph transcode["Transcode Worker (Go + FFmpeg)"]
        direction TB
        T1[Download raw video] --> T2[Encode 360p + 720p + 1080p]
        T2 --> T3[Generate HLS playlist]
        T3 --> T4[Upload to MinIO]
    end

    subgraph whisper["Whisper Worker (Python)"]
        direction TB
        W1[Download raw video] --> W2[Extract audio]
        W2 --> W3[Whisper: transcribe]
        W3 --> W4[Generate SRT file]
        W4 --> W5[Upload to MinIO]
    end

    subgraph thumbnail["Thumbnail Worker (Python + OpenCV)"]
        direction TB
        TH1[Download raw video] --> TH2[Extract 10 frames]
        TH2 --> TH3[Score: sharpness + entropy]
        TH3 --> TH4[Pick top 5, mark best]
        TH4 --> TH5[Upload to MinIO]
    end
```

all three workers run at the same time. they don't wait for each other.

### phase 3: store results

each worker uploads its output to MinIO and updates PostgreSQL with the status and file paths.

```mermaid
flowchart TD
    A[Transcode Worker] -->|HLS segments + playlists| D[(MinIO)]
    B[Whisper Worker] -->|SRT caption file| D
    C[Thumbnail Worker] -->|thumbnail images| D

    A -->|transcode_status: completed| E[(PostgreSQL)]
    B -->|caption_status: completed| E
    C -->|thumbnail_status: completed| E
```

### phase 4: serve and notify

the dashboard polls PostgreSQL for status updates. once all three workers finish, the video status changes to "completed" and a webhook fires (if configured).

```mermaid
flowchart TD
    A[(PostgreSQL)] -->|all 3 statuses = completed| B[Video status: completed]
    B --> C[React Dashboard shows results]
    B --> D[Webhook POST to your app]
    C --> E[HLS video player with captions]
    C --> F[Thumbnail gallery]
    C --> G[Full transcript]
```

the dashboard auto-refreshes every 3 seconds while processing. when done, you can play the adaptive stream, view captions, and browse thumbnails.

## what each worker does (in detail)

### transcode worker (Go + FFmpeg)

takes the raw upload and produces [HLS](https://en.wikipedia.org/wiki/HTTP_Live_Streaming) adaptive streams. the video player automatically switches quality based on the viewer's connection - exactly like YouTube.

| quality | resolution | bitrate | when it's used |
|---|---|---|---|
| low | 640x360 | 800 kbps | slow wifi, mobile data |
| medium | 1280x720 | 2.5 Mbps | normal playback |
| high | 1920x1080 | 5 Mbps | good connection, fullscreen |

outputs a master `.m3u8` playlist. any HLS player (Safari, hls.js, VLC, mobile apps) handles quality switching automatically.

### whisper worker (Python + OpenAI Whisper)

runs [OpenAI Whisper](https://github.com/openai/whisper) locally (no API key needed - the model runs inside the container). extracts audio, transcribes it, generates SRT subtitle files with timestamps.

- handles accents, background noise, and crosstalk
- auto-detects the language (40+ languages supported)
- base model runs in ~1x real-time on CPU (60s video = ~60s to caption)
- output: `.srt` file with timestamped segments

this is the same model that powers most AI transcription tools - but running on your hardware, for free.

### thumbnail worker (Python + OpenCV)

extracts 10 frames at equal intervals and scores each one:

- **sharpness** - laplacian variance (blurry frames get penalized)
- **entropy** - histogram entropy (black frames and solid colors score near zero)
- **content quality** - penalizes too-dark and too-bright frames

picks top 5 candidates, marks the winner. no more black-frame thumbnails, no more "uploading a video and manually screenshotting at 0:42."

## quickstart

```bash
git clone https://github.com/Nixxx19/vidpipe.git
cd vidpipe
docker compose up --build
```

open http://localhost:3000 and upload a video. watch it flow through the pipeline.

### services

| service | port | what |
|---|---|---|
| dashboard | http://localhost:3000 | upload, watch, browse |
| api | http://localhost:8080 | REST API |
| minio console | http://localhost:9001 | browse stored files |
| postgres | 5432 | metadata |
| redis | 6379 | job queue |

minio credentials: `vidpipe` / `vidpipe123`

## api

```bash
# upload a video
curl -X POST http://localhost:8080/api/upload -F "file=@video.mp4"

# response:
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "filename": "video.mp4",
  "status": "uploaded",
  "duration": 127.4,
  "width": 1920,
  "height": 1080
}

# list all videos
curl http://localhost:8080/api/videos

# get video with full metadata
curl http://localhost:8080/api/videos/{id}

# response includes:
#   transcode_status: pending -> processing -> completed
#   caption_status:   pending -> processing -> completed
#   thumbnail_status: pending -> processing -> completed
#   hls_path:         path to HLS master playlist
#   caption_path:     path to SRT file
#   caption_text:     full transcript
#   caption_language: detected language code
#   thumbnail_path:   best thumbnail
#   thumbnail_candidates: all scored thumbnails

# stream the video (HLS)
curl http://localhost:8080/api/videos/{id}/stream

# real-time progress (Server-Sent Events)
curl http://localhost:8080/api/videos/{id}/events

# system health + queue stats
curl http://localhost:8080/api/health
```

### webhooks

set the `WEBHOOK_URL` environment variable in docker-compose.yml. when all three workers finish processing a video, vidpipe sends a POST request with the full video JSON to your URL.

```bash
# example: notify your app when processing completes
WEBHOOK_URL=https://your-app.com/api/video-ready
```

use this to trigger downstream actions - update your UI, send an email, index the transcript for search.

## architecture decisions

**why redis streams, not kafka** - kafka is designed for millions of events per second across a distributed cluster. we're processing video uploads. redis streams give us consumer groups, message acknowledgment, and pending message recovery - all the guarantees we need, with zero additional infrastructure.

**why minio, not the filesystem** - every production deployment uses S3-compatible storage. minio means the same code works with AWS S3, Google Cloud Storage, Cloudflare R2, or DigitalOcean Spaces. change one environment variable.

**why go + python, not all one language** - go handles the I/O-heavy parts (upload, streaming, FFmpeg process management) where goroutines shine. python handles the ML parts (whisper, opencv) where the ecosystem is 10 years ahead. each worker is a separate container - scale them independently.

**why hls, not dash** - safari plays HLS natively. everything else uses hls.js (a 50KB library). dash needs a larger player library and has worse mobile support.

**why consumer groups, not pub/sub** - each job must be processed exactly once. if a worker crashes mid-transcode, the message stays in the pending list and gets picked up by another worker. no lost jobs, no duplicate processing.

**why whisper runs locally** - no API key, no per-minute billing, no data leaving your network. the base model is 140MB and runs on CPU. for a self-hosted tool, this matters.

## failure handling

vidpipe doesn't lose jobs. here's what happens when things go wrong:

```mermaid
flowchart TB
    A[Worker picks up job] --> B{Processing succeeds?}
    B -->|yes| C[Update DB: completed]
    C --> D[XACK: remove from queue]
    B -->|no / crash| E[Message stays in pending list]
    E --> F{Idle > 5 minutes?}
    F -->|yes| G[Another worker XCLAIMs it]
    G --> H{Retry count < 3?}
    H -->|yes| A
    H -->|no| I[Mark as failed, stop retrying]
    F -->|no| J[Wait for original worker]
```

- if a worker crashes mid-job, the message stays pending in Redis
- after 5 minutes idle, another worker automatically reclaims it
- after 3 failed attempts, the job is marked as failed (no infinite loops)

## tech stack

| layer | tech | role |
|---|---|---|
| api server | Go + Fiber | upload, video listing, HLS proxy, SSE |
| transcode | Go + FFmpeg | raw -> HLS (360p/720p/1080p) |
| captions | Python + Whisper | speech-to-text, SRT generation |
| thumbnails | Python + OpenCV | frame extraction + quality scoring |
| job queue | Redis Streams | parallel job distribution, consumer groups |
| object storage | MinIO | S3-compatible, stores all files |
| database | PostgreSQL | metadata, status tracking |
| dashboard | React + Vite + Tailwind | upload UI, HLS player, status monitoring |
| deployment | Docker Compose | one command, 6 containers |

## who uses something like this

- **course platforms** - upload a lecture, get captions + streaming automatically (Udemy, Coursera all have this)
- **internal tools** - company training videos, meeting recordings with searchable transcripts
- **video-first products** - any app with user-uploaded video needs this pipeline
- **content platforms** - TikTok/YouTube-style apps need transcoding + thumbnails at scale
- **accessibility compliance** - auto-generated captions for WCAG/ADA requirements

## what makes this different from a FFmpeg script

| | typical tutorial | vidpipe |
|---|---|---|
| processing | one FFmpeg command, runs synchronously | 3 parallel workers via message queue |
| captions | none | Whisper AI, auto-detected language |
| thumbnails | first frame or manual | quality-scored frame selection |
| storage | local filesystem | S3-compatible (MinIO/AWS/GCS) |
| failure handling | crashes and you lose the file | consumer groups, automatic retry (3 attempts) |
| scaling | single process | each worker scales independently |
| monitoring | none | dashboard with real-time status + SSE |
| notifications | none | webhook POST when done |
| metadata | none | ffprobe extraction (duration, resolution, codec) |
| deployment | "run this script" | `docker compose up` |

## project structure

```
vidpipe/
├── docker-compose.yml              6 services, one command
│
├── api/                            Go API server
│   ├── main.go                     routes, startup, middleware
│   ├── handlers/
│   │   ├── upload.go               multipart upload + ffprobe metadata
│   │   ├── videos.go               CRUD + webhooks + SSE + health
│   │   └── stream.go               HLS proxy from MinIO
│   ├── queue/redis.go              Redis Streams job producer
│   ├── storage/minio.go            S3 file operations
│   └── db/
│       ├── postgres.go             connection pool
│       └── models.go               Video model + queries
│
├── workers/
│   ├── transcode/                  Go + FFmpeg
│   │   ├── main.go                 HLS transcoding (3 qualities) + retry logic
│   │   └── Dockerfile              alpine + ffmpeg
│   ├── whisper/                    Python + Whisper
│   │   ├── main.py                 speech-to-text + SRT generation
│   │   └── Dockerfile              python + ffmpeg + whisper
│   └── thumbnail/                  Python + OpenCV
│       ├── main.py                 frame scoring + selection
│       └── Dockerfile              python + opencv
│
├── dashboard/                      React + Vite + Tailwind
│   └── src/
│       ├── pages/
│       │   ├── Upload.tsx          drag-and-drop upload
│       │   ├── VideoList.tsx       grid with live status
│       │   └── VideoDetail.tsx     player + captions + thumbnails
│       └── components/
│           ├── VideoPlayer.tsx     HLS.js adaptive player
│           ├── StatusBadge.tsx     animated status indicators
│           └── UploadDropzone.tsx  file drop zone
│
└── db/init.sql                     PostgreSQL schema
```
