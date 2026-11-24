import { afterEach, expect, it, vi } from "vitest";
import { api, tokenKey } from "./api";
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
});
it("sends session token and surfaces server error without discarding message", async () => {
  sessionStorage.setItem(tokenKey, "  admin-test  ");
  const fetchMock = vi.fn(async () => ({
    ok: false,
    status: 409,
    json: async () => ({ error: "已有进行中的直播" }),
  }));
  vi.stubGlobal("fetch", fetchMock);
  await expect(api("/live", "POST", {})).rejects.toThrow("已有进行中的直播");
  expect(fetchMock.mock.calls[0]).toEqual([
    "/api/live",
    {
      method: "POST",
      headers: {
        Authorization: "Bearer admin-test",
        "Content-Type": "application/json",
      },
      body: "{}",
    },
  ]);
});
it("handles empty success bodies", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok: true,
      status: 200,
      text: async () => "",
      json: async () => {
        throw new SyntaxError("Unexpected end of JSON input");
      },
    })),
  );
  await expect(api("/live/s/stop", "POST")).resolves.toBeUndefined();
});
