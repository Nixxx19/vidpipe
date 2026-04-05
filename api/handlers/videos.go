package handlers

import (
	"database/sql"
	"log"

	"github.com/gofiber/fiber/v2"

	"streamforge/api/db"
)

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
