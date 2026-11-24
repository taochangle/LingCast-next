import { useEffect, useState } from "react";
import { api, ApiError } from "../api";

export function hostError(error: unknown) {
  const status = error instanceof ApiError ? error.status : 0;
  switch (status) {
    case 400:
      return "输入内容无效，请检查字数和重复内容后重试。";
    case 401:
      return "管理员令牌未通过验证，请重新连接管理台。";
    case 409:
      return "状态已变化，请重新读取后再试。";
    case 429:
      return "主题正在生成或暂时限流，请稍后重试。";
    case 503:
      return "服务暂不可用，请检查服务设置后重试。";
    default:
      return "请求未完成，请检查连接后重试。";
  }
}

// Callers key their view by resource. Schedule each read only after the previous one settles.
export function useHostRead<T>(path: string) {
  const [data, setData] = useState<T>();
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  function reportError(e: unknown) {
    if (e instanceof ApiError && e.status === 401) setData(undefined);
    setError(hostError(e));
  }
  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    async function read() {
      try {
        const next = await api<T>(path, "GET", undefined, controller.signal);
        if (active) {
          setData(next);
          setError("");
        }
      } catch (e) {
        if (active) reportError(e);
      } finally {
        if (active) timer = setTimeout(read, 3000);
      }
    }
    void read();
    return () => {
      active = false;
      controller.abort();
      clearTimeout(timer);
    };
  }, [path, revision]);
  return {
    data,
    error,
    reportError,
    refresh: () => setRevision((value) => value + 1),
  };
}
