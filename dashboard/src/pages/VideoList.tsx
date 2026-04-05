import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import axios from "axios";
import StatusBadge from "../components/StatusBadge";

interface Video {
  id: string;
  title: string;
  filename: string;
  thumbnail_path: string | null;
  transcode_status: string;
  caption_status: string;
  thumbnail_status: string;
  created_at: string;
}

function VideoList() {
  const [videos, setVideos] = useState<Video[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchVideos = async () => {
      try {
        const response = await axios.get("/api/videos");
        setVideos(response.data);
      } catch (err: any) {
        setError(err.message || "Failed to load videos");
      } finally {
        setLoading(false);
      }
    };
    fetchVideos();
  }, []);

  if (loading) {
    return (
      <div className="flex justify-center py-20">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-500" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-4 bg-red-900/30 border border-red-800 rounded-lg text-red-300">
        {error}
      </div>
    );
  }

  if (videos.length === 0) {
    return (
      <div className="text-center py-20">
        <p className="text-gray-400 text-lg mb-4">No videos yet</p>
        <Link
          to="/upload"
          className="bg-indigo-600 hover:bg-indigo-700 text-white px-6 py-2 rounded-lg text-sm font-medium transition-colors"
        >
          Upload your first video
        </Link>
      </div>
    );
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Videos</h1>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-6">
        {videos.map((video) => (
          <Link
            key={video.id}
            to={`/videos/${video.id}`}
            className="bg-gray-900 border border-gray-800 rounded-xl overflow-hidden hover:border-gray-700 transition-colors group"
          >
            <div className="aspect-video bg-gray-800 flex items-center justify-center overflow-hidden">
              {video.thumbnail_path ? (
                <img
                  src={`/api/files/${video.thumbnail_path}`}
                  alt={video.title || video.filename}
                  className="w-full h-full object-cover group-hover:scale-105 transition-transform"
                />
              ) : (
                <svg
                  className="w-12 h-12 text-gray-600"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={1.5}
                    d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z"
                  />
                </svg>
              )}
            </div>
            <div className="p-4">
              <h3 className="font-medium text-gray-200 truncate mb-2">
                {video.title || video.filename}
              </h3>
              <div className="flex flex-wrap gap-1.5">
                <StatusBadge status={video.transcode_status} label="Transcode" />
                <StatusBadge status={video.caption_status} label="Caption" />
                <StatusBadge status={video.thumbnail_status} label="Thumbnail" />
              </div>
              <p className="text-xs text-gray-500 mt-2">
                {new Date(video.created_at).toLocaleDateString()}
              </p>
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}

export default VideoList;
