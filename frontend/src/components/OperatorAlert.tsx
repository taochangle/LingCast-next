const messages: Record<string, string> = {
  "Worker start failed":
    "直播未能启动。请到服务设置检查直播处理服务，确认就绪后重新开播。",
  "Worker start failed or cancelled":
    "直播未能启动。请到服务设置检查直播处理服务，确认就绪后重新开播。",
  "Start cancelled": "本次开播已取消。请刷新状态后重新开播。",
  "Worker unavailable": "暂时无法连接直播处理服务。请检查服务连接后重试。",
  "Relay update failed": "转推设置未能更新。请检查服务连接，再确认转推状态。",
  "Default idle required": "请先到人物与素材中指定一条默认口型底片。",
  "Video conversion failed": "视频处理失败。请换一个可正常播放的视频后重试。",
  "Converted video exceeds size limit": "处理后的视频超过 200 MiB，请缩短视频后重试。",
  Unauthorized:
    "管理员令牌未通过验证。请断开管理连接，使用正确的令牌重新连接。",
};

export function operatorMessage(detail: string) {
  return (
    messages[detail] ??
    (/[\u3400-\u9fff]/u.test(detail)
      ? detail
      : "请求未完成。请刷新状态后重试，或查看错误详情。")
  );
}

export default function OperatorAlert({
  detail,
  onSettings,
  onClose,
  busy,
}: {
  detail: string;
  onSettings?: () => void;
  onClose?: () => void;
  busy: boolean;
}) {
  const message = operatorMessage(detail);
  return (
    <div role="alert" className="alert operator-alert">
      <p>{message}</p>
      {message !== detail && (
        <details>
          <summary>错误详情</summary>
          <p>{detail}</p>
        </details>
      )}
      <div className="actions">
        {onSettings && (
          <button disabled={busy} onClick={onSettings}>
            前往服务设置
          </button>
        )}
        {onClose && (
          <button className="text" onClick={onClose}>
            关闭
          </button>
        )}
      </div>
    </div>
  );
}
