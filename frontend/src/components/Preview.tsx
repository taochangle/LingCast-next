import { useEffect, useRef, useState } from "react";
import type mpegtsType from "mpegts.js";
export default function Preview({ url }: { url: string }) {
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
    <div className="preview">
      {url ? (
        <video ref={ref} controls playsInline aria-label="直播画面预览" />
      ) : (
        <div className="preview-empty">
          <div className="frame-mark" />
          <h2>等待直播画面</h2>
          <p>完成素材准备后开播，画面将在这里显示。</p>
        </div>
      )}
      {error && <p role="alert">{error}</p>}
    </div>
  );
}
