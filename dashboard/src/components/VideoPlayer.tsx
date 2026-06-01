import { useEffect, useRef } from "react";
import Hls from "hls.js";

interface VideoPlayerProps {
  src: string;
  captionSrc?: string | null;
  captionLang?: string | null;
}

function VideoPlayer({ src, captionSrc, captionLang }: VideoPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    if (Hls.isSupported()) {
      const hls = new Hls({
        enableWorker: true,
        lowLatencyMode: false,
      });

      hls.loadSource(src);
      hls.attachMedia(video);
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        video.play().catch(() => {});
      });

      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) {
          switch (data.type) {
            case Hls.ErrorTypes.NETWORK_ERROR:
              console.error("Network error, trying to recover...");
              hls.startLoad();
              break;
            case Hls.ErrorTypes.MEDIA_ERROR:
              console.error("Media error, trying to recover...");
              hls.recoverMediaError();
              break;
            default:
              console.error("Fatal error, destroying HLS instance");
              hls.destroy();
              break;
          }
        }
      });

      hlsRef.current = hls;

      return () => {
        hls.destroy();
        hlsRef.current = null;
      };
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = src;
      video.addEventListener("loadedmetadata", () => {
        video.play().catch(() => {});
      });
    }
  }, [src]);

  // chrome ignores the <track default> attr when react injects it, so force
  // the caption track to "showing" once it's attached.
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !captionSrc) return;

    const showCaptions = () => {
      for (let i = 0; i < video.textTracks.length; i++) {
        video.textTracks[i].mode = "showing";
      }
    };

    showCaptions();
    video.textTracks.addEventListener("addtrack", showCaptions);
    return () => video.textTracks.removeEventListener("addtrack", showCaptions);
  }, [captionSrc]);

  return (
    <div className="aspect-video bg-black rounded-xl overflow-hidden">
      <video
        ref={videoRef}
        className="w-full h-full"
        controls
        playsInline
      >
        {captionSrc && (
          <track
            kind="captions"
            src={captionSrc}
            srcLang={captionLang || "en"}
            label="Captions"
            default
          />
        )}
      </video>
    </div>
  );
}

export default VideoPlayer;
