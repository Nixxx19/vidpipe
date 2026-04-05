package handlers

import (
	"database/sql"
	"fmt"
	"log"

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
