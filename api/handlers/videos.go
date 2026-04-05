package handlers

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	"streamforge/api/db"
)

// SSEDeps holds dependencies for the SSE progress endpoint.
type SSEDeps struct {
	DB *sql.DB
}

// HealthDeps holds dependencies for the health/stats endpoint.
type HealthDeps struct {
	DB          *sql.DB
	RedisClient *redis.Client
}

func HandleListVideos(database *sql.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		videos, err := db.ListVideos(database)
		if err != nil {
			log.Printf("failed to list videos: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to retrieve videos",
			})
		}

		if videos == nil {
			videos = []db.Video{}
		}

		return c.JSON(fiber.Map{
			"videos": videos,
			"count":  len(videos),
		})
	}
}

func CheckAndNotifyCompletion(database *sql.DB, videoID string) {
	allDone, err := db.CheckAllCompleted(database, videoID)
	if err != nil {
		log.Printf("failed to check completion for video %s: %v", videoID, err)
		return
	}
	if !allDone {
		return
	}

	if err := db.UpdateVideoField(database, videoID, "status", "completed"); err != nil {
		log.Printf("failed to update video %s status to completed: %v", videoID, err)
		return
	}
	log.Printf("video %s: all workers completed, status set to completed", videoID)

	webhookURL := os.Getenv("WEBHOOK_URL")
	if webhookURL == "" {
		return
	}

	video, err := db.GetVideo(database, videoID)
	if err != nil || video == nil {
		log.Printf("failed to fetch video %s for webhook: %v", videoID, err)
		return
	}

	payload, err := json.Marshal(video)
	if err != nil {
		log.Printf("failed to marshal video %s for webhook: %v", videoID, err)
		return
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("failed to send webhook for video %s: %v", videoID, err)
		return
	}
	defer resp.Body.Close()
	log.Printf("webhook sent for video %s, response status: %d", videoID, resp.StatusCode)
}

func HandleVideoSSE(deps *SSEDeps) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")

		// Verify video exists
		video, err := db.GetVideo(deps.DB, id)
		if err != nil {
			log.Printf("SSE: failed to get video %s: %v", id, err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to retrieve video",
			})
		}
		if video == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "video not found",
			})
		}

		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")

		c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					v, err := db.GetVideo(deps.DB, id)
					if err != nil {
						log.Printf("SSE: error fetching video %s: %v", id, err)
						return
					}
					if v == nil {
						return
					}

					data, err := json.Marshal(v)
					if err != nil {
						log.Printf("SSE: error marshalling video %s: %v", id, err)
						return
					}

					fmt.Fprintf(w, "data: %s\n\n", data)
					if err := w.Flush(); err != nil {
						// Client disconnected
						return
					}

					if v.Status == "completed" || v.Status == "failed" {
						return
					}
				}
			}
		})

		return nil
	}
}

func HandleHealth(deps *HealthDeps) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := context.Background()

		result := fiber.Map{
			"status":  "ok",
			"service": "streamforge-api",
		}

		// Queue depth: XLEN of "video-jobs"
		queueDepth, err := deps.RedisClient.XLen(ctx, "video-jobs").Result()
		if err != nil {
			log.Printf("Health: XLEN error: %v", err)
			queueDepth = -1
		}
		result["queue_depth"] = queueDepth

		// Pending messages per consumer group
		pendingInfo, err := deps.RedisClient.XInfoGroups(ctx, "video-jobs").Result()
		if err != nil {
			log.Printf("Health: XINFO GROUPS error: %v", err)
			result["pending_messages"] = nil
		} else {
			pending := fiber.Map{}
			for _, group := range pendingInfo {
				pending[group.Name] = group.Pending
			}
			result["pending_messages"] = pending
		}

		// Video counts from DB
		var totalVideos, completedVideos, failedVideos, processingVideos int64

		if err := deps.DB.QueryRow("SELECT COUNT(*) FROM videos").Scan(&totalVideos); err != nil {
			log.Printf("Health: total_videos query error: %v", err)
		}
		result["total_videos"] = totalVideos

		if err := deps.DB.QueryRow("SELECT COUNT(*) FROM videos WHERE status = 'completed'").Scan(&completedVideos); err != nil {
			log.Printf("Health: completed_videos query error: %v", err)
		}
		result["completed_videos"] = completedVideos

		if err := deps.DB.QueryRow("SELECT COUNT(*) FROM videos WHERE status = 'failed'").Scan(&failedVideos); err != nil {
			log.Printf("Health: failed_videos query error: %v", err)
		}
		result["failed_videos"] = failedVideos

		if err := deps.DB.QueryRow("SELECT COUNT(*) FROM videos WHERE status != 'completed' AND status != 'failed' AND status != 'uploaded'").Scan(&processingVideos); err != nil {
			log.Printf("Health: processing_videos query error: %v", err)
		}
		result["processing_videos"] = processingVideos

		return c.JSON(result)
	}
}

func HandleGetVideo(database *sql.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")

		video, err := db.GetVideo(database, id)
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

		return c.JSON(video)
	}
}
