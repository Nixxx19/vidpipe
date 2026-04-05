import os
import signal
import sys
import tempfile
import time
from urllib.parse import urlparse

import boto3
import psycopg2
import redis
import whisper

shutdown_requested = False


def handle_signal(signum, frame):
    global shutdown_requested
    print(f"Received signal {signum}, shutting down gracefully...")
    shutdown_requested = True


signal.signal(signal.SIGTERM, handle_signal)
signal.signal(signal.SIGINT, handle_signal)


def parse_database_url(url: str) -> dict:
    parsed = urlparse(url)
    return {
        "host": parsed.hostname,
        "port": parsed.port or 5432,
        "dbname": parsed.path.lstrip("/"),
        "user": parsed.username,
        "password": parsed.password,
    }


def get_pg_connection():
    db_params = parse_database_url(os.environ["DATABASE_URL"])
    return psycopg2.connect(**db_params)


def get_redis_connection():
    return redis.Redis.from_url(os.environ["REDIS_URL"], decode_responses=True)


def get_s3_client():
    return boto3.client(
        "s3",
        endpoint_url=os.environ["MINIO_ENDPOINT"],
        aws_access_key_id=os.environ["MINIO_ACCESS_KEY"],
        aws_secret_access_key=os.environ["MINIO_SECRET_KEY"],
        region_name="us-east-1",
    )


def generate_srt(segments: list) -> str:
    srt_lines = []
    for i, seg in enumerate(segments, start=1):
        start = format_timestamp(seg["start"])
        end = format_timestamp(seg["end"])
        text = seg["text"].strip()
        srt_lines.append(f"{i}")
        srt_lines.append(f"{start} --> {end}")
        srt_lines.append(text)
        srt_lines.append("")
    return "\n".join(srt_lines)


def format_timestamp(seconds: float) -> str:
    hrs = int(seconds // 3600)
    mins = int((seconds % 3600) // 60)
    secs = int(seconds % 60)
    millis = int((seconds - int(seconds)) * 1000)
    return f"{hrs:02d}:{mins:02d}:{secs:02d},{millis:03d}"


def update_status(conn, video_id: str, status: str, extra_fields: dict = None):
    fields = ["caption_status = %s"]
    values = [status]
    if extra_fields:
        for key, val in extra_fields.items():
            fields.append(f"{key} = %s")
            values.append(val)
    values.append(video_id)
    query = f"UPDATE videos SET {', '.join(fields)} WHERE id = %s"
    with conn.cursor() as cur:
        cur.execute(query, values)
    conn.commit()


def process_caption_job(video_id: str, storage_path: str, model):
    conn = get_pg_connection()
    s3 = get_s3_client()
    bucket = os.environ["MINIO_BUCKET"]

    try:
        update_status(conn, video_id, "processing")

        with tempfile.NamedTemporaryFile(suffix=".mp4", delete=False) as tmp:
            tmp_path = tmp.name
            s3.download_file(bucket, storage_path, tmp_path)

        print(f"Transcribing video {video_id}...")
        result = model.transcribe(tmp_path)

        srt_content = generate_srt(result["segments"])
        caption_path = f"captions/{video_id}.srt"

        srt_tmp = tmp_path + ".srt"
        with open(srt_tmp, "w") as f:
            f.write(srt_content)

        s3.upload_file(srt_tmp, bucket, caption_path)

        full_text = result["text"].strip()
        language = result.get("language", "en")

        update_status(conn, video_id, "completed", {
            "caption_path": caption_path,
            "caption_text": full_text,
            "caption_language": language,
        })

        print(f"Caption completed for video {video_id}, language: {language}")

        os.unlink(tmp_path)
        os.unlink(srt_tmp)

    except Exception as e:
        print(f"Error processing caption for {video_id}: {e}")
        try:
            update_status(conn, video_id, "failed")
        except Exception:
            pass
        raise
    finally:
        conn.close()


def main():
    print("Starting Whisper Caption Worker...")

    r = get_redis_connection()
    stream = "video-jobs"
    group = "whisper-workers"
    consumer = f"whisper-{os.getpid()}"

    try:
        r.xgroup_create(stream, group, id="0", mkstream=True)
        print(f"Created consumer group '{group}'")
    except redis.exceptions.ResponseError as e:
        if "BUSYGROUP" not in str(e):
            raise
        print(f"Consumer group '{group}' already exists")

    print("Loading Whisper model (base)...")
    model = whisper.load_model("base")
    print("Model loaded. Listening for caption jobs...")

    while not shutdown_requested:
        try:
            messages = r.xreadgroup(
                group, consumer, {stream: ">"}, count=1, block=5000
            )

            if not messages:
                continue

            for stream_name, entries in messages:
                for msg_id, data in entries:
                    job_type = data.get("job_type", "")
                    if job_type != "caption":
                        r.xack(stream, group, msg_id)
                        continue

                    video_id = data.get("video_id", "")
                    storage_path = data.get("storage_path", "")

                    if not video_id or not storage_path:
                        print(f"Invalid message {msg_id}: missing video_id or storage_path")
                        r.xack(stream, group, msg_id)
                        continue

                    print(f"Processing caption job for video {video_id}")
                    try:
                        process_caption_job(video_id, storage_path, model)
                    except Exception as e:
                        print(f"Failed to process {video_id}: {e}")

                    r.xack(stream, group, msg_id)

        except redis.exceptions.ConnectionError:
            print("Redis connection lost, reconnecting in 5s...")
            time.sleep(5)
            r = get_redis_connection()
        except Exception as e:
            print(f"Unexpected error: {e}")
            time.sleep(1)

    print("Whisper Caption Worker shut down.")


if __name__ == "__main__":
    main()
