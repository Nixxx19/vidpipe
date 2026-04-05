import io
import json
import os
import signal
import tempfile
import time
from urllib.parse import urlparse

import boto3
import cv2
import numpy as np
import psycopg2
import redis
from PIL import Image

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


def update_status(conn, video_id: str, status: str, extra_fields: dict = None):
    fields = ["thumbnail_status = %s"]
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


def compute_sharpness(frame: np.ndarray) -> float:
    gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
    laplacian = cv2.Laplacian(gray, cv2.CV_64F)
    return float(laplacian.var())


def compute_entropy(frame: np.ndarray) -> float:
    gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
    hist = cv2.calcHist([gray], [0], None, [256], [0, 256])
    hist = hist.flatten()
    hist = hist / hist.sum()
    hist = hist[hist > 0]
    return float(-np.sum(hist * np.log2(hist)))


def compute_content_score(frame: np.ndarray) -> float:
    gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
    std_dev = float(np.std(gray))
    mean_val = float(np.mean(gray))

    brightness_penalty = 0.0
    if mean_val < 30 or mean_val > 225:
        brightness_penalty = -2.0

    contrast_bonus = min(std_dev / 50.0, 1.0)
    return contrast_bonus + brightness_penalty


def score_frame(frame: np.ndarray) -> float:
    sharpness = compute_sharpness(frame)
    entropy = compute_entropy(frame)
    content = compute_content_score(frame)

    sharpness_norm = min(sharpness / 500.0, 1.0)
    entropy_norm = entropy / 8.0

    return sharpness_norm * 0.4 + entropy_norm * 0.4 + content * 0.2


def extract_frames(video_path: str, num_frames: int = 10) -> list:
    cap = cv2.VideoCapture(video_path)
    if not cap.isOpened():
        raise RuntimeError(f"Cannot open video: {video_path}")

    total_frames = int(cap.get(cv2.CAP_PROP_FRAME_COUNT))
    if total_frames < num_frames:
        num_frames = max(total_frames, 1)

    interval = total_frames // (num_frames + 1)
    frames = []

    for i in range(1, num_frames + 1):
        frame_idx = i * interval
        cap.set(cv2.CAP_PROP_POS_FRAMES, frame_idx)
        ret, frame = cap.read()
        if ret:
            frames.append((frame_idx, frame))

    cap.release()
    return frames


def frame_to_jpeg_bytes(frame: np.ndarray, quality: int = 90) -> bytes:
    rgb = cv2.cvtColor(frame, cv2.COLOR_BGR2RGB)
    img = Image.fromarray(rgb)
    buf = io.BytesIO()
    img.save(buf, format="JPEG", quality=quality)
    return buf.getvalue()


def process_thumbnail_job(video_id: str, storage_path: str):
    conn = get_pg_connection()
    s3 = get_s3_client()
    bucket = os.environ["MINIO_BUCKET"]

    try:
        update_status(conn, video_id, "processing")

        with tempfile.NamedTemporaryFile(suffix=".mp4", delete=False) as tmp:
            tmp_path = tmp.name
            s3.download_file(bucket, storage_path, tmp_path)

        print(f"Extracting frames from video {video_id}...")
        frames = extract_frames(tmp_path, num_frames=10)

        if not frames:
            raise RuntimeError("No frames could be extracted from video")

        scored = []
        for frame_idx, frame in frames:
            sc = score_frame(frame)
            scored.append((sc, frame_idx, frame))

        scored.sort(key=lambda x: x[0], reverse=True)
        top_5 = scored[:5]

        uploaded_paths = []
        best_path = None

        for rank, (sc, frame_idx, frame) in enumerate(top_5):
            jpeg_bytes = frame_to_jpeg_bytes(frame)
            thumb_key = f"thumbnails/{video_id}/thumb_{rank}.jpg"

            s3.put_object(
                Bucket=bucket,
                Key=thumb_key,
                Body=jpeg_bytes,
                ContentType="image/jpeg",
            )
            uploaded_paths.append(thumb_key)

            if rank == 0:
                best_path = thumb_key

            print(f"  Uploaded {thumb_key} (score={sc:.4f}, frame={frame_idx})")

        candidates_json = json.dumps(uploaded_paths)

        update_status(conn, video_id, "completed", {
            "thumbnail_path": best_path,
            "thumbnail_candidates": candidates_json,
        })

        print(f"Thumbnail generation completed for video {video_id}")

        os.unlink(tmp_path)

    except Exception as e:
        print(f"Error processing thumbnail for {video_id}: {e}")
        try:
            update_status(conn, video_id, "failed")
        except Exception:
            pass
        raise
    finally:
        conn.close()


def main():
    print("Starting Thumbnail Worker...")

    r = get_redis_connection()
    stream = "video-jobs"
    group = "thumbnail-workers"
    consumer = f"thumbnail-{os.getpid()}"

    try:
        r.xgroup_create(stream, group, id="0", mkstream=True)
        print(f"Created consumer group '{group}'")
    except redis.exceptions.ResponseError as e:
        if "BUSYGROUP" not in str(e):
            raise
        print(f"Consumer group '{group}' already exists")

    print("Listening for thumbnail jobs...")

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
                    if job_type != "thumbnail":
                        r.xack(stream, group, msg_id)
                        continue

                    video_id = data.get("video_id", "")
                    storage_path = data.get("storage_path", "")

                    if not video_id or not storage_path:
                        print(f"Invalid message {msg_id}: missing video_id or storage_path")
                        r.xack(stream, group, msg_id)
                        continue

                    print(f"Processing thumbnail job for video {video_id}")
                    try:
                        process_thumbnail_job(video_id, storage_path)
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

    print("Thumbnail Worker shut down.")


if __name__ == "__main__":
    main()
