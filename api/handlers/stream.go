package handlers

import (
	"database/sql"
	"io"
	"log"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/minio/minio-go/v7"

	"streamforge/api/db"
	"streamforge/api/storage"
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
	default:
		return "application/octet-stream"
	}
}
