package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"

	"streamforge/api/db"
	"streamforge/api/queue"
	"streamforge/api/storage"
)

type UploadDeps struct {
	DB          *sql.DB
	MinioClient *minio.Client
	RedisClient *redis.Client
	Bucket      string
}

func HandleUpload(deps *UploadDeps) fiber.Handler {
	return func(c *fiber.Ctx) error {
		file, err := c.FormFile("file")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "file is required",
			})
		}

		videoID := uuid.New().String()
		objectName := fmt.Sprintf("raw/%s/%s", videoID, file.Filename)

		src, err := file.Open()
		if err != nil {
			log.Printf("failed to open uploaded file: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to read uploaded file",
			})
		}
		defer src.Close()

		contentType := file.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		if err := storage.UploadFile(deps.MinioClient, deps.Bucket, objectName, src, file.Size, contentType); err != nil {
			log.Printf("failed to upload to minio: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to store file",
			})
		}

		video := &db.Video{
			ID:              videoID,
			Filename:        file.Filename,
			OriginalPath:    objectName,
			FileSize:        file.Size,
			MimeType:        contentType,
			Status:          "pending",
			TranscodeStatus: "pending",
			CaptionStatus:   "pending",
			ThumbnailStatus: "pending",
		}

		if err := db.CreateVideo(deps.DB, video); err != nil {
			log.Printf("failed to create video record: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to save video metadata",
			})
		}

		// Extract metadata via ffprobe
		if meta, err := extractMetadata(deps.MinioClient, deps.Bucket, objectName); err != nil {
			log.Printf("failed to extract metadata for video %s: %v", videoID, err)
		} else {
			if meta.Duration != nil {
				if err := db.UpdateVideoField(deps.DB, videoID, "duration", *meta.Duration); err != nil {
					log.Printf("failed to update duration for video %s: %v", videoID, err)
				}
			}
			if meta.Width != nil {
				if err := db.UpdateVideoField(deps.DB, videoID, "width", *meta.Width); err != nil {
					log.Printf("failed to update width for video %s: %v", videoID, err)
				}
			}
			if meta.Height != nil {
				if err := db.UpdateVideoField(deps.DB, videoID, "height", *meta.Height); err != nil {
					log.Printf("failed to update height for video %s: %v", videoID, err)
				}
			}
		}

		jobTypes := []string{"transcode", "caption", "thumbnail"}
		for _, jobType := range jobTypes {
			if err := queue.PublishJob(deps.RedisClient, videoID, jobType); err != nil {
				log.Printf("failed to publish %s job for video %s: %v", jobType, videoID, err)
			}
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"id":       video.ID,
			"filename": video.Filename,
			"status":   video.Status,
			"message":  "video uploaded successfully, processing started",
		})
	}
}

type videoMeta struct {
	Duration *float64
	Width    *int
	Height   *int
	Codec    *string
}

type ffprobeOutput struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecName string `json:"codec_name"`
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
}

func extractMetadata(minioClient *minio.Client, bucket, objectName string) (*videoMeta, error) {
	obj, err := storage.GetFile(minioClient, bucket, objectName)
	if err != nil {
		return nil, fmt.Errorf("failed to download from minio: %w", err)
	}
	defer obj.Close()

	tmpFile, err := os.CreateTemp("", "vidpipe-probe-*.mp4")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, obj); err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", tmpFile.Name())
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var probe ffprobeOutput
	if err := json.Unmarshal(output, &probe); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	meta := &videoMeta{}

	if probe.Format.Duration != "" {
		if d, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
			meta.Duration = &d
		}
	}

	for _, s := range probe.Streams {
		if s.CodecType == "video" {
			meta.Width = &s.Width
			meta.Height = &s.Height
			codec := s.CodecName
			meta.Codec = &codec
			break
		}
	}

	return meta, nil
}
