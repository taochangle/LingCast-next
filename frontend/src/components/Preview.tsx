import MediaImage from "./MediaImage";
import { MonitorPlay } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type mpegtsType from "mpegts.js";
export default function Preview({
  url,
  imageId,
}: {
  url: string;
  imageId: string;
}) {
  const ref = useRef<HTMLVideoElement>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let disposed = false;
    let player: mpegtsType.Player | undefined;
    setError("");
    if (!url || !ref.current) return;
    import("mpegts.js")
      .then(({ default: mpegts }) => {
        if (disposed || !ref.current) return;
        if (!mpegts.isSupported()) {
          setError("当前浏览器不支持 FLV 预览，请使用桌面 Chrome 或 Edge。");
          return;
        }
        player = mpegts.createPlayer({ type: "flv", isLive: true, url });
        player.attachMediaElement(ref.current);
        player.on(mpegts.Events.ERROR, () =>
          setError("预览连接中断，请检查服务状态或重新连接。"),
        );
        player.load();
      })
      .catch(() => setError("预览组件加载失败，请刷新页面。"));
    return () => {
      disposed = true;
      if (player) {
        player.unload();
        player.detachMediaElement();
        player.destroy();
      }
    };
  }, [url]);
  return (
    <div className={url ? "preview playing" : "preview"}>
      {url ? (
        <video ref={ref} controls playsInline aria-label="直播画面预览" />
      ) : (
        <div className="preview-empty">
          <MonitorPlay aria-hidden="true" />
          <strong>等待直播画面</strong>
          <p>选好人物与形象，开始直播后在这里查看画面。</p>
        </div>
      )}
      {!url && imageId && (
        <MediaImage id={imageId} alt="所选形象预览" className="poster" />
      )}
      {error && <p role="alert">{error}</p>}
    </div>
  );
}
