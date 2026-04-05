package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"streamforge/api/db"
	"streamforge/api/handlers"
	"streamforge/api/queue"
	"streamforge/api/storage"
)

func main() {
	// Read configuration from environment
	databaseURL := getEnv("DATABASE_URL", "postgres://streamforge:streamforge@localhost:5432/streamforge?sslmode=disable")
	redisURL := getEnv("REDIS_URL", "redis://localhost:6379")
	minioBucket := getEnv("MINIO_BUCKET", "streamforge")

	// Initialize PostgreSQL
	database, err := db.InitDB(databaseURL)
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}
	defer database.Close()
	log.Println("connected to PostgreSQL")

	// Initialize Redis
	redisClient, err := queue.InitRedis(redisURL)
	if err != nil {
		log.Fatalf("failed to initialize redis: %v", err)
	}
	defer redisClient.Close()
	log.Println("connected to Redis")

	// Initialize MinIO
	minioClient, err := storage.InitMinio()
	if err != nil {
		log.Fatalf("failed to initialize minio: %v", err)
	}
	log.Println("connected to MinIO")

	// Ensure bucket exists
	if err := storage.EnsureBucket(minioClient, minioBucket); err != nil {
		log.Fatalf("failed to ensure minio bucket: %v", err)
	}
	log.Printf("MinIO bucket %q ready", minioBucket)

	// Create Fiber app
	app := fiber.New(fiber.Config{
		BodyLimit: 500 * 1024 * 1024, // 500MB upload limit
	})

	// Middleware
	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders: "Origin,Content-Type,Accept,Authorization",
	}))

	// Dependencies
	uploadDeps := &handlers.UploadDeps{
		DB:          database,
		MinioClient: minioClient,
		RedisClient: redisClient,
		Bucket:      minioBucket,
	}

	streamDeps := &handlers.StreamDeps{
		DB:          database,
		MinioClient: minioClient,
		Bucket:      minioBucket,
	}

	sseDeps := &handlers.SSEDeps{
		DB: database,
	}

	healthDeps := &handlers.HealthDeps{
		DB:          database,
		RedisClient: redisClient,
	}

	// Routes
	api := app.Group("/api")

	api.Post("/upload", handlers.HandleUpload(uploadDeps))
	api.Get("/videos", handlers.HandleListVideos(database))
	api.Get("/videos/:id", handlers.HandleGetVideo(database))
	api.Get("/videos/:id/stream", handlers.HandleStream(streamDeps))
	api.Get("/videos/:id/events", handlers.HandleVideoSSE(sseDeps))

	app.Get("/api/health", handlers.HandleHealth(healthDeps))

	// Start server
	port := getEnv("PORT", "8080")
	log.Printf("StreamForge API starting on :%s", port)
	if err := app.Listen(":" + port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
