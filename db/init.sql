CREATE TABLE IF NOT EXISTS videos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    filename VARCHAR(255) NOT NULL,
    original_path VARCHAR(512) NOT NULL,
    file_size BIGINT NOT NULL,
    mime_type VARCHAR(100),
    duration FLOAT,
    width INT,
    height INT,
    status VARCHAR(50) DEFAULT 'uploaded',
    transcode_status VARCHAR(50) DEFAULT 'pending',
    caption_status VARCHAR(50) DEFAULT 'pending',
    thumbnail_status VARCHAR(50) DEFAULT 'pending',
    hls_path VARCHAR(512),
    caption_path VARCHAR(512),
    caption_text TEXT,
    caption_language VARCHAR(10),
    thumbnail_path VARCHAR(512),
    thumbnail_candidates TEXT[],
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_videos_status ON videos(status);
CREATE INDEX idx_videos_created ON videos(created_at DESC);
