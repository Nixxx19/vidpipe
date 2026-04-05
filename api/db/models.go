package db

import (
	"database/sql"
	"fmt"
	"time"
)

type Video struct {
	ID               string     `json:"id"`
	Filename         string     `json:"filename"`
	OriginalPath     string     `json:"original_path"`
	FileSize         int64      `json:"file_size"`
	MimeType         string     `json:"mime_type"`
	Duration         *float64   `json:"duration"`
	Width            *int       `json:"width"`
	Height           *int       `json:"height"`
	Status           string     `json:"status"`
	TranscodeStatus  string     `json:"transcode_status"`
	CaptionStatus    string     `json:"caption_status"`
	ThumbnailStatus  string     `json:"thumbnail_status"`
	HLSPath          *string    `json:"hls_path"`
	CaptionPath      *string    `json:"caption_path"`
	CaptionText      *string    `json:"caption_text"`
	CaptionLanguage  *string    `json:"caption_language"`
	ThumbnailPath    *string    `json:"thumbnail_path"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func CreateVideo(db *sql.DB, v *Video) error {
	query := `
		INSERT INTO videos (id, filename, original_path, file_size, mime_type, status, transcode_status, caption_status, thumbnail_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at, updated_at`

	return db.QueryRow(
		query,
		v.ID, v.Filename, v.OriginalPath, v.FileSize, v.MimeType,
		v.Status, v.TranscodeStatus, v.CaptionStatus, v.ThumbnailStatus,
	).Scan(&v.CreatedAt, &v.UpdatedAt)
}

func GetVideo(db *sql.DB, id string) (*Video, error) {
	v := &Video{}
	query := `
		SELECT id, filename, original_path, file_size, mime_type,
		       duration, width, height, status, transcode_status,
		       caption_status, thumbnail_status, hls_path, caption_path,
		       caption_text, caption_language, thumbnail_path, created_at, updated_at
		FROM videos WHERE id = $1`

	err := db.QueryRow(query, id).Scan(
		&v.ID, &v.Filename, &v.OriginalPath, &v.FileSize, &v.MimeType,
		&v.Duration, &v.Width, &v.Height, &v.Status, &v.TranscodeStatus,
		&v.CaptionStatus, &v.ThumbnailStatus, &v.HLSPath, &v.CaptionPath,
		&v.CaptionText, &v.CaptionLanguage, &v.ThumbnailPath, &v.CreatedAt, &v.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get video: %w", err)
	}
	return v, nil
}

func ListVideos(db *sql.DB) ([]Video, error) {
	query := `
		SELECT id, filename, original_path, file_size, mime_type,
		       duration, width, height, status, transcode_status,
		       caption_status, thumbnail_status, hls_path, caption_path,
		       caption_text, caption_language, thumbnail_path, created_at, updated_at
		FROM videos ORDER BY created_at DESC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to list videos: %w", err)
	}
	defer rows.Close()

	var videos []Video
	for rows.Next() {
		var v Video
		if err := rows.Scan(
			&v.ID, &v.Filename, &v.OriginalPath, &v.FileSize, &v.MimeType,
			&v.Duration, &v.Width, &v.Height, &v.Status, &v.TranscodeStatus,
			&v.CaptionStatus, &v.ThumbnailStatus, &v.HLSPath, &v.CaptionPath,
			&v.CaptionText, &v.CaptionLanguage, &v.ThumbnailPath, &v.CreatedAt, &v.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan video row: %w", err)
		}
		videos = append(videos, v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating video rows: %w", err)
	}

	return videos, nil
}

func CheckAllCompleted(db *sql.DB, videoID string) (bool, error) {
	var transcode, caption, thumbnail string
	query := `SELECT transcode_status, caption_status, thumbnail_status FROM videos WHERE id = $1`
	err := db.QueryRow(query, videoID).Scan(&transcode, &caption, &thumbnail)
	if err != nil {
		return false, fmt.Errorf("failed to check completion statuses: %w", err)
	}
	return transcode == "completed" && caption == "completed" && thumbnail == "completed", nil
}

func UpdateVideoField(db *sql.DB, id string, field string, value interface{}) error {
	allowedFields := map[string]bool{
		"status":           true,
		"transcode_status": true,
		"caption_status":   true,
		"thumbnail_status": true,
		"hls_path":         true,
		"caption_path":     true,
		"caption_text":     true,
		"caption_language":  true,
		"thumbnail_path":   true,
		"duration":         true,
		"width":            true,
		"height":           true,
	}

	if !allowedFields[field] {
		return fmt.Errorf("field %q is not allowed for update", field)
	}

	query := fmt.Sprintf(`UPDATE videos SET %s = $1, updated_at = NOW() WHERE id = $2`, field)
	result, err := db.Exec(query, value, id)
	if err != nil {
		return fmt.Errorf("failed to update video field %s: %w", field, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("video %s not found", id)
	}

	return nil
}
