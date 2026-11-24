export const tokenKey = "lingcast.admin";
export class ApiError extends Error {
  constructor(
    public status: number,
    detail: string,
  ) {
    super(detail);
  }
}
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const headers: Record<string, string> = {
    Authorization: `Bearer ${(sessionStorage.getItem(tokenKey) || "").trim()}`,
  };
  if (body !== undefined && !(body instanceof FormData))
    headers["Content-Type"] = "application/json";
  const res = await fetch(`/api${path}`, {
    method,
    ...(signal && { signal }),
    headers,
    body:
      body === undefined
        ? undefined
        : body instanceof FormData
          ? body
          : JSON.stringify(body),
  });
  if (!res.ok) {
    let message = `请求失败（${res.status}）`;
    try {
      const data = await res.json();
      message = data.error || message;
    } catch {}
    throw new ApiError(res.status, message);
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}
export async function upload(file: File): Promise<import("./types").Media> {
  const form = new FormData();
  form.append("file", file);
  return api("/media", "POST", form);
}
