import { useEffect, useState, useCallback } from "react";
import { useParams } from "react-router-dom";
import axios from "axios";
import VideoPlayer from "../components/VideoPlayer";
import StatusBadge from "../components/StatusBadge";

interface Video {
  id: string;
  title: string;
  filename: string;
  storage_path: string;
  hls_path: string | null;
  thumbnail_path: string | null;
  thumbnail_candidates: string[] | null;
  caption_path: string | null;
  caption_text: string | null;
  caption_language: string | null;
  transcode_status: string;
  caption_status: string;
  thumbnail_status: string;
  created_at: string;
}

function VideoDetail() {
  const { id } = useParams<{ id: string }>();
  const [video, setVideo] = useState<Video | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchVideo = useCallback(async () => {
    try {
      const response = await axios.get(`/api/videos/${id}`);
      setVideo(response.data);
      setError(null);
    } catch (err: any) {
      setError(err.message || "Failed to load video");
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    fetchVideo();
  }, [fetchVideo]);

  useEffect(() => {
    if (!video) return;

    const isProcessing =
      video.transcode_status === "processing" ||
      video.transcode_status === "pending" ||
      video.caption_status === "processing" ||
      video.caption_status === "pending" ||
      video.thumbnail_status === "processing" ||
      video.thumbnail_status === "pending";

    if (!isProcessing) return;

    const interval = setInterval(fetchVideo, 3000);
    return () => clearInterval(interval);
  }, [video, fetchVideo]);

  if (loading) {
    return (
      <div className="flex justify-center py-20">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-500" />
      </div>
    );
  }

  if (error || !video) {
    return (
      <div className="p-4 bg-red-900/30 border border-red-800 rounded-lg text-red-300">
        {error || "Video not found"}
      </div>
    );
  }

  const hlsUrl = video.hls_path ? `/api/files/${video.hls_path}` : null;

  return (
    <div className="max-w-4xl mx-auto">
      <h1 className="text-2xl font-bold mb-2">
        {video.title || video.filename}
      </h1>
      <p className="text-sm text-gray-500 mb-6">
        Uploaded {new Date(video.created_at).toLocaleString()}
      </p>

      {/* Video Player */}
      <div className="mb-8">
        {hlsUrl ? (
          <VideoPlayer src={hlsUrl} />
        ) : (
          <div className="aspect-video bg-gray-900 rounded-xl flex items-center justify-center border border-gray-800">
            <div className="text-center">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-500 mx-auto mb-3" />
              <p className="text-gray-400 text-sm">
                Video is being transcoded...
              </p>
            </div>
          </div>
        )}
      </div>

      {/* Processing Status */}
      <div className="bg-gray-900 border border-gray-800 rounded-xl p-6 mb-8">
        <h2 className="text-lg font-semibold mb-4">Processing Status</h2>
        <div className="grid grid-cols-3 gap-4">
          <div className="text-center">
            <p className="text-sm text-gray-400 mb-2">Transcode</p>
            <StatusBadge status={video.transcode_status} />
          </div>
          <div className="text-center">
            <p className="text-sm text-gray-400 mb-2">Captions</p>
            <StatusBadge status={video.caption_status} />
          </div>
          <div className="text-center">
            <p className="text-sm text-gray-400 mb-2">Thumbnails</p>
            <StatusBadge status={video.thumbnail_status} />
          </div>
        </div>
      </div>

      {/* Caption Download */}
      {video.caption_path && (
        <div className="bg-gray-900 border border-gray-800 rounded-xl p-6 mb-8">
          <h2 className="text-lg font-semibold mb-4">Captions</h2>
          <div className="flex items-center justify-between">
            <div>
              {video.caption_language && (
                <p className="text-sm text-gray-400">
                  Language: {video.caption_language}
                </p>
              )}
            </div>
            <a
              href={`/api/files/${video.caption_path}`}
              download
              className="bg-gray-800 hover:bg-gray-700 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors flex items-center gap-2"
            >
              <svg
                className="w-4 h-4"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M12 10v6m0 0l-3-3m3 3l3-3m2 8H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
                />
              </svg>
              Download SRT
            </a>
          </div>
          {video.caption_text && (
            <div className="mt-4 p-4 bg-gray-800 rounded-lg max-h-40 overflow-y-auto">
              <p className="text-sm text-gray-300 whitespace-pre-wrap">
                {video.caption_text}
              </p>
            </div>
          )}
        </div>
      )}

      {/* Thumbnail Gallery */}
      {video.thumbnail_candidates && video.thumbnail_candidates.length > 0 && (
        <div className="bg-gray-900 border border-gray-800 rounded-xl p-6 mb-8">
          <h2 className="text-lg font-semibold mb-4">Thumbnails</h2>
          <div className="grid grid-cols-5 gap-3">
            {video.thumbnail_candidates.map((path, i) => (
              <div
                key={i}
                className={`aspect-video rounded-lg overflow-hidden border-2 ${
                  path === video.thumbnail_path
                    ? "border-indigo-500"
                    : "border-transparent"
                }`}
              >
                <img
                  src={`/api/files/${path}`}
                  alt={`Thumbnail ${i + 1}`}
                  className="w-full h-full object-cover"
                />
                {path === video.thumbnail_path && (
                  <div className="text-center">
                    <span className="text-xs text-indigo-400 font-medium">
                      Best
                    </span>
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export default VideoDetail;
