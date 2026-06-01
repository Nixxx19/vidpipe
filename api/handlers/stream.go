package handlers

import (
	"database/sql"
	"io"
	"log"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"

	"vidpipe/api/db"
	"vidpipe/api/storage"
)

type StreamDeps struct {
	DB          *sql.DB
	MinioClient *minio.Client
	Bucket      string
}

func HandleStream(deps *StreamDeps) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")

		video, err := db.GetVideo(deps.DB, id)
		if err != nil {
			log.Printf("failed to get video %s: %v", id, err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to retrieve video",
			})
		}

		if video == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "video not found",
			})
		}

		if video.HLSPath == nil || *video.HLSPath == "" {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "HLS stream not available yet",
			})
		}

		// Allow requesting specific segment files via ?file= query param
		// Default to the main playlist
		requestedFile := c.Query("file", "")
		var objectName string

		if requestedFile != "" {
			// Serve a specific segment or sub-playlist
			dir := filepath.Dir(*video.HLSPath)
			objectName = dir + "/" + filepath.Base(requestedFile)
		} else {
			objectName = *video.HLSPath
		}

		obj, err := storage.GetFile(deps.MinioClient, deps.Bucket, objectName)
		if err != nil {
			log.Printf("failed to get HLS file %s: %v", objectName, err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to retrieve stream",
			})
		}
		defer obj.Close()

		contentType := resolveHLSContentType(objectName)
		c.Set("Content-Type", contentType)
		c.Set("Cache-Control", "no-cache")
		c.Set("Access-Control-Allow-Origin", "*")

		data, err := io.ReadAll(obj)
		if err != nil {
			log.Printf("failed to read HLS file %s: %v", objectName, err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to read stream data",
			})
		}

		return c.Send(data)
	}
}

func resolveHLSContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	case ".mp4":
		return "video/mp4"
	case ".vtt":
		return "text/vtt"
	case ".srt":
		return "application/x-subrip"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	default:
		return "application/octet-stream"
	}
}

// HandleFiles streams any processed artifact straight from MinIO by its object
// key (e.g. hls/<id>/master.m3u8, thumbnails/<id>/thumb_0.jpg, captions/<id>.srt).
// The dashboard references all of these under /api/files/<key>.
func HandleFiles(deps *StreamDeps) fiber.Handler {
	return func(c *fiber.Ctx) error {
		objectName := c.Params("*")
		if objectName == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "file path is required",
			})
		}
		// Reject path traversal attempts.
		if strings.Contains(objectName, "..") {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid file path",
			})
		}

		obj, err := storage.GetFile(deps.MinioClient, deps.Bucket, objectName)
		if err != nil {
			log.Printf("failed to get file %s: %v", objectName, err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to retrieve file",
			})
		}
		defer obj.Close()

		// Stat first so a missing object returns 404 instead of a stream error.
		if _, err := obj.Stat(); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "file not found",
			})
		}

		c.Set("Content-Type", resolveHLSContentType(objectName))
		c.Set("Cache-Control", "public, max-age=3600")
		c.Set("Access-Control-Allow-Origin", "*")

		data, err := io.ReadAll(obj)
		if err != nil {
			log.Printf("failed to read file %s: %v", objectName, err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to read file",
			})
		}

		return c.Send(data)
	}
}
