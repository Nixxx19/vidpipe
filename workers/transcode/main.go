package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
)

const (
	streamName    = "video-jobs"
	groupName     = "transcode-workers"
	consumerName  = "transcode-worker-1"
	jobTypeField  = "job_type"
	jobTypeTarget = "transcode"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		cancel()
	}()

	// Connect to Redis
	redisOpts, err := redis.ParseURL(mustEnv("REDIS_URL"))
	if err != nil {
		log.Fatalf("Invalid REDIS_URL: %v", err)
	}
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Redis connection failed: %v", err)
	}
	log.Println("Connected to Redis")

	// Connect to PostgreSQL
	db, err := sql.Open("postgres", mustEnv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("PostgreSQL connection failed: %v", err)
	}
	log.Println("Connected to PostgreSQL")

	// Connect to MinIO
	useSSL := false
	if v := os.Getenv("MINIO_USE_SSL"); v == "true" || v == "1" {
		useSSL = true
	}

	minioClient, err := minio.New(mustEnv("MINIO_ENDPOINT"), &minio.Options{
		Creds:  credentials.NewStaticV4(mustEnv("MINIO_ACCESS_KEY"), mustEnv("MINIO_SECRET_KEY"), ""),
		Secure: useSSL,
	})
	if err != nil {
		log.Fatalf("MinIO connection failed: %v", err)
	}

	bucket := mustEnv("MINIO_BUCKET")

	// Ensure bucket exists
	exists, err := minioClient.BucketExists(ctx, bucket)
	if err != nil {
		log.Fatalf("Failed to check bucket: %v", err)
	}
	if !exists {
		if err := minioClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			log.Fatalf("Failed to create bucket: %v", err)
		}
	}
	log.Println("Connected to MinIO")

	// Create consumer group (ignore BUSYGROUP error if it already exists)
	err = rdb.XGroupCreateMkStream(ctx, streamName, groupName, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		log.Fatalf("Failed to create consumer group: %v", err)
	}
	log.Printf("Consumer group '%s' ready on stream '%s'", groupName, streamName)

	// Main processing loop
	log.Println("Waiting for transcode jobs...")
	for {
		select {
		case <-ctx.Done():
			log.Println("Worker stopped")
			return
		default:
		}

		streams, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    groupName,
			Consumer: consumerName,
			Streams:  []string{streamName, ">"},
			Count:    1,
			Block:    5 * time.Second,
		}).Result()
		if err != nil {
			if err == redis.Nil || ctx.Err() != nil {
				continue
			}
			log.Printf("XReadGroup error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				jobType, _ := msg.Values[jobTypeField].(string)
				if jobType != jobTypeTarget {
					// Not a transcode job; acknowledge and skip
					rdb.XAck(ctx, streamName, groupName, msg.ID)
					continue
				}

				videoID, _ := msg.Values["video_id"].(string)
				if videoID == "" {
					log.Printf("Message %s missing video_id, skipping", msg.ID)
					rdb.XAck(ctx, streamName, groupName, msg.ID)
					continue
				}

				log.Printf("Processing transcode job for video %s (msg %s)", videoID, msg.ID)

				if err := processTranscode(ctx, db, minioClient, bucket, videoID); err != nil {
					log.Printf("Transcode failed for video %s: %v", videoID, err)
					updateStatus(ctx, db, videoID, "failed", "")
				}

				rdb.XAck(ctx, streamName, groupName, msg.ID)
			}
		}
	}
}

func processTranscode(ctx context.Context, db *sql.DB, mc *minio.Client, bucket, videoID string) error {
	// Update status to processing
	if err := updateStatus(ctx, db, videoID, "processing", ""); err != nil {
		return fmt.Errorf("update status to processing: %w", err)
	}

	// Get original path from database
	var originalPath string
	err := db.QueryRowContext(ctx,
		"SELECT original_path FROM videos WHERE id = $1", videoID,
	).Scan(&originalPath)
	if err != nil {
		return fmt.Errorf("query video: %w", err)
	}

	// Create temp working directory
	tmpDir, err := os.MkdirTemp("", "transcode-"+videoID+"-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Download raw video from MinIO
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := downloadFromMinio(ctx, mc, bucket, originalPath, inputPath); err != nil {
		return fmt.Errorf("download video: %w", err)
	}
	log.Printf("Downloaded %s to %s", originalPath, inputPath)

	// Run FFmpeg transcoding
	outputPattern := filepath.Join(tmpDir, "output_%v.m3u8")
	segmentPattern := filepath.Join(tmpDir, "output_%v_%03d.ts")

	args := []string{
		"-i", inputPath,
		"-map", "0:v", "-map", "0:a",
		"-map", "0:v", "-map", "0:a",
		"-map", "0:v", "-map", "0:a",
		"-c:v", "libx264",
		"-c:a", "aac",
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_segment_filename", segmentPattern,
		"-var_stream_map", "v:0,a:0 v:1,a:1 v:2,a:2",
		"-filter:v:0", "scale=640:360",
		"-b:v:0", "800k",
		"-filter:v:1", "scale=1280:720",
		"-b:v:1", "2500k",
		"-filter:v:2", "scale=1920:1080",
		"-b:v:2", "5000k",
		outputPattern,
	}

	log.Printf("Running FFmpeg for video %s", videoID)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Dir = tmpDir
	cmdOutput, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w\nOutput: %s", err, string(cmdOutput))
	}
	log.Printf("FFmpeg completed for video %s", videoID)

	// Generate master playlist referencing all quality variants
	masterPlaylist := generateMasterPlaylist(videoID)
	masterPath := filepath.Join(tmpDir, "master.m3u8")
	if err := os.WriteFile(masterPath, []byte(masterPlaylist), 0644); err != nil {
		return fmt.Errorf("write master playlist: %w", err)
	}

	// Upload all .m3u8 and .ts files to MinIO under hls/{videoID}/
	hlsPrefix := fmt.Sprintf("hls/%s", videoID)
	uploadCount, err := uploadHLSFiles(ctx, mc, bucket, tmpDir, hlsPrefix)
	if err != nil {
		return fmt.Errorf("upload HLS files: %w", err)
	}
	log.Printf("Uploaded %d files to %s/%s", uploadCount, bucket, hlsPrefix)

	// Update database with completed status and HLS path
	hlsPath := fmt.Sprintf("%s/master.m3u8", hlsPrefix)
	if err := updateStatus(ctx, db, videoID, "completed", hlsPath); err != nil {
		return fmt.Errorf("update status to completed: %w", err)
	}

	log.Printf("Transcode completed for video %s", videoID)
	return nil
}

func downloadFromMinio(ctx context.Context, mc *minio.Client, bucket, objectKey, destPath string) error {
	obj, err := mc.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer obj.Close()

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.ReadFrom(obj); err != nil {
		return err
	}
	return nil
}

func generateMasterPlaylist(videoID string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")

	// 360p
	b.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360\n")
	b.WriteString("output_0.m3u8\n")

	// 720p
	b.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720\n")
	b.WriteString("output_1.m3u8\n")

	// 1080p
	b.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080\n")
	b.WriteString("output_2.m3u8\n")

	return b.String()
}

func uploadHLSFiles(ctx context.Context, mc *minio.Client, bucket, srcDir, prefix string) (int, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".m3u8") && !strings.HasSuffix(name, ".ts") {
			continue
		}

		localPath := filepath.Join(srcDir, name)
		objectKey := fmt.Sprintf("%s/%s", prefix, name)

		contentType := "application/octet-stream"
		if strings.HasSuffix(name, ".m3u8") {
			contentType = "application/vnd.apple.mpegurl"
		} else if strings.HasSuffix(name, ".ts") {
			contentType = "video/mp2t"
		}

		info, err := entry.Info()
		if err != nil {
			return count, fmt.Errorf("stat %s: %w", name, err)
		}

		f, err := os.Open(localPath)
		if err != nil {
			return count, fmt.Errorf("open %s: %w", name, err)
		}

		_, err = mc.PutObject(ctx, bucket, objectKey, f, info.Size(), minio.PutObjectOptions{
			ContentType: contentType,
		})
		f.Close()
		if err != nil {
			return count, fmt.Errorf("upload %s: %w", objectKey, err)
		}
		count++
	}
	return count, nil
}

func updateStatus(ctx context.Context, db *sql.DB, videoID, status, hlsPath string) error {
	var err error
	if hlsPath != "" {
		_, err = db.ExecContext(ctx,
			"UPDATE videos SET transcode_status = $1, hls_path = $2, updated_at = NOW() WHERE id = $3",
			status, hlsPath, videoID,
		)
	} else {
		_, err = db.ExecContext(ctx,
			"UPDATE videos SET transcode_status = $1, updated_at = NOW() WHERE id = $2",
			status, videoID,
		)
	}
	if err != nil {
		return fmt.Errorf("update transcode_status to %s: %w", status, err)
	}
	return nil
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("Required environment variable %s is not set", key)
	}
	return v
}
