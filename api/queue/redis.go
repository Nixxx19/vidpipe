package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

func InitRedis(url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis URL: %w", err)
	}

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	return client, nil
}

func PublishJob(client *redis.Client, videoID string, jobType string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: "video-jobs",
		Values: map[string]interface{}{
			"video_id": videoID,
			"job_type": jobType,
		},
	}).Result()
	if err != nil {
		return fmt.Errorf("failed to publish %s job for video %s: %w", jobType, videoID, err)
	}

	return nil
}
