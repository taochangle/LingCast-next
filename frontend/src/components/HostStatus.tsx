import { useHostRead } from "./useHostRead";

const states: Record<string, string> = {
  waiting: "等待主持",
  preparing: "准备中",
  paused: "已暂停",
  speaking: "口播已提交",
  degraded: "主持降级，稍后自动重试",
};
export default function HostStatus({ sessionId }: { sessionId: string }) {
  return <SessionStatus key={sessionId} sessionId={sessionId} />;
}
function SessionStatus({ sessionId }: { sessionId: string }) {
  const { data, error, refresh } = useHostRead<{
    state: string;
    topicTitle: string;
  }>(`/live/${sessionId}/host-status`);
  return (
    <div className="host-status">
      {error ? (
        <div role="alert">
          {error} <button onClick={refresh}>重新读取</button>
        </div>
      ) : (
        <>
          <strong>主持状态</strong>{" "}
          <span>{data ? states[data.state] || "状态未知" : "读取中…"}</span>
          {data?.topicTitle && (
            <div>
              当前话题 <span>{data.topicTitle}</span>
            </div>
          )}
        </>
      )}
    </div>
  );
}
