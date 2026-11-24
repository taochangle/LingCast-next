import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import App from "./App";
const calls: { url: string; body: any }[] = [];
beforeEach(() => {
  sessionStorage.setItem("lingcast.admin", "secret");
  calls.length = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options: any = {}) => {
      calls.push({ url, body: options.body ? JSON.parse(options.body) : null });
      let data: any = [];
      if (url === "/api/avatars")
        data = [
          { id: "a", name: "小林", persona: "", voiceId: "v" },
          { id: "b", name: "小夏", persona: "", voiceId: "v" },
        ];
      if (url.endsWith("/looks"))
        data = url.includes("/a/")
          ? [
              {
                id: "la",
                avatarId: "a",
                name: "春装",
                imageId: "img",
                defaultIdleId: "i",
              },
            ]
          : [
              {
                id: "lb",
                avatarId: "b",
                name: "夏装",
                imageId: "img",
                defaultIdleId: "j",
              },
            ];
      if (url === "/api/live")
        data =
          options.method === "POST"
            ? {
                session: {
                  id: "s",
                  avatarId: "a",
                  lookId: "la",
                  platform: "douyin",
                  status: "running",
                  hostEnabled: true,
                },
                collectorToken: "token",
              }
            : { session: null };
      return {
        ok: true,
        status: 200,
        json: async () => data,
        text: async () => JSON.stringify(data),
      };
    }),
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  sessionStorage.clear();
});
it("requires selections and scopes looks to avatar; start includes platform without automatic relay", async () => {
  render(<App />);
  const start = await screen.findByRole("button", { name: "开始直播" });
  expect(start).toBeDisabled();
  fireEvent.change(screen.getByLabelText("直播人物"), {
    target: { value: "a" },
  });
  await screen.findByRole("option", { name: "春装" });
  expect(
    screen.queryByRole("option", { name: "夏装" }),
  ).not.toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("直播形象"), {
    target: { value: "la" },
  });
  fireEvent.change(screen.getByLabelText("直播平台"), {
    target: { value: "xiaohongshu" },
  });
  expect(screen.getByLabelText("本场主持模式")).toHaveValue("fixed");
  fireEvent.change(screen.getByLabelText("本场主持模式"), {
    target: { value: "companion" },
  });
  fireEvent.click(start);
  await waitFor(() =>
    expect(
      calls.some(
        (c) =>
          c.url === "/api/live" &&
          c.body?.platform === "xiaohongshu" &&
          c.body?.hostMode === "companion",
      ),
    ).toBe(true),
  );
  expect(calls.some((c) => c.url.endsWith("/relay"))).toBe(false);
});
it("shows API errors", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok: false,
      status: 503,
      json: async () => ({ error: "Worker 暂未就绪" }),
    })),
  );
  render(<App />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Worker 暂未就绪");
});
it("stop, host and relay need explicit clicks", async () => {
  render(<App />);
  fireEvent.change(await screen.findByLabelText("直播人物"), {
    target: { value: "a" },
  });
  await screen.findByRole("option", { name: "春装" });
  fireEvent.change(screen.getByLabelText("直播形象"), {
    target: { value: "la" },
  });
  fireEvent.click(screen.getByRole("button", { name: "开始直播" }));
  await screen.findByRole("button", { name: "结束直播" });
  fireEvent.click(screen.getByRole("button", { name: "暂停主持" }));
  await waitFor(() =>
    expect(
      calls.some((c) => c.url.endsWith("/host") && c.body.enabled === false),
    ).toBe(true),
  );
  fireEvent.click(screen.getByRole("button", { name: "结束直播" }));
  await waitFor(() =>
    expect(calls.some((c) => c.url.endsWith("/stop"))).toBe(true),
  );
});
it("clears look selection when changing avatars", async () => {
  render(<App />);
  fireEvent.change(await screen.findByLabelText("直播人物"), {
    target: { value: "a" },
  });
  await screen.findByRole("option", { name: "春装" });
  fireEvent.change(screen.getByLabelText("直播形象"), {
    target: { value: "la" },
  });
  fireEvent.change(screen.getByLabelText("直播人物"), {
    target: { value: "b" },
  });
  await screen.findByRole("option", { name: "夏装" });
  expect(screen.getByLabelText("直播形象")).toHaveValue("");
  expect(
    screen.queryByRole("option", { name: "春装" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "开始直播" })).toBeDisabled();
});
it("only sends relay credentials after explicit enable", async () => {
  render(<App />);
  fireEvent.change(await screen.findByLabelText("直播人物"), {
    target: { value: "a" },
  });
  await screen.findByRole("option", { name: "春装" });
  fireEvent.change(screen.getByLabelText("直播形象"), {
    target: { value: "la" },
  });
  fireEvent.click(screen.getByRole("button", { name: "开始直播" }));
  await screen.findByRole("button", { name: "结束直播" });
  fireEvent.click(screen.getByText("平台转推"));
  fireEvent.change(screen.getByLabelText("RTMP / RTMPS 服务地址"), {
    target: { value: "rtmps://example.invalid/live" },
  });
  fireEvent.change(screen.getByLabelText("推流密钥"), {
    target: { value: "test-key" },
  });
  expect(calls.some((c) => c.url.endsWith("/relay"))).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "启用转推" }));
  await waitFor(() =>
    expect(
      calls.some((c) => c.url.endsWith("/relay") && c.body.enabled === true),
    ).toBe(true),
  );
});
it("ignores a delayed live poll begun before start", async () => {
  let tick!: () => void;
  const realInterval = globalThis.setInterval;
  vi.spyOn(globalThis, "setInterval").mockImplementation(((
    fn: () => void,
    ms: number,
  ) => {
    if (ms === 5000) {
      tick = fn;
      return 1;
    }
    return realInterval(fn, ms);
  }) as any);
  const original = globalThis.fetch;
  let resolvePoll!: (v: any) => void;
  let liveGets = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, options: any = {}) => {
      if (url === "/api/live" && options.method !== "POST") {
        liveGets++;
        if (liveGets === 2)
          return new Promise((r) => {
            resolvePoll = r;
          });
      }
      return original(url, options);
    }),
  );
  render(<App />);
  fireEvent.change(await screen.findByLabelText("直播人物"), {
    target: { value: "a" },
  });
  await screen.findByRole("option", { name: "春装" });
  fireEvent.change(screen.getByLabelText("直播形象"), {
    target: { value: "la" },
  });
  tick();
  await waitFor(() => expect(resolvePoll).toBeDefined());
  fireEvent.click(screen.getByRole("button", { name: "开始直播" }));
  await screen.findByRole("button", { name: "结束直播" });
  resolvePoll({
    ok: true,
    status: 200,
    text: async () => JSON.stringify({ session: null }),
  });
  await new Promise((r) => setTimeout(r, 0));
  expect(screen.getByRole("button", { name: "结束直播" })).toBeInTheDocument();
});
it("does not show delayed scripts from a previous avatar", async () => {
  const original = globalThis.fetch;
  let resolveA!: (v: any) => void;
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, options: any = {}) => {
      if (url === "/api/avatars/a/scripts")
        return new Promise((r) => {
          resolveA = r;
        });
      if (url === "/api/avatars/b/scripts")
        return Promise.resolve({
          ok: true,
          status: 200,
          text: async () =>
            JSON.stringify([
              { id: "sb", avatarId: "b", kind: "host", text: "B 正确稿件" },
            ]),
        });
      return original(url, options);
    }),
  );
  render(<App />);
  await screen.findByRole("option", { name: "小林" });
  fireEvent.click(screen.getByRole("button", { name: "话术库" }));
  fireEvent.change(screen.getByLabelText("所属人物"), {
    target: { value: "a" },
  });
  fireEvent.change(screen.getByLabelText("所属人物"), {
    target: { value: "b" },
  });
  await screen.findByText("B 正确稿件");
  resolveA({
    ok: true,
    status: 200,
    text: async () =>
      JSON.stringify([
        { id: "sa", avatarId: "a", kind: "host", text: "A 过期稿件" },
      ]),
  });
  await new Promise((r) => setTimeout(r, 0));
  expect(screen.queryByText("A 过期稿件")).not.toBeInTheDocument();
  expect(screen.getByText("B 正确稿件")).toBeInTheDocument();
});
it("rejects delayed look and idle lists after changing selection", async () => {
  const original = globalThis.fetch;
  let resolveLooks!: (v: any) => void;
  let resolveIdles!: (v: any) => void;
  const response = (data: any) => ({
    ok: true,
    status: 200,
    text: async () => JSON.stringify(data),
  });
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, options: any = {}) => {
      if (url === "/api/avatars/a/looks")
        return new Promise((r) => {
          resolveLooks = r;
        });
      if (url === "/api/avatars/b/looks")
        return Promise.resolve(
          response([
            { id: "lb1", avatarId: "b", name: "B 形象一", imageId: "img" },
            { id: "lb2", avatarId: "b", name: "B 形象二", imageId: "img" },
          ]),
        );
      if (url === "/api/looks/lb1/idles")
        return new Promise((r) => {
          resolveIdles = r;
        });
      if (url === "/api/looks/lb2/idles")
        return Promise.resolve(
          response([{ id: "ib2", lookId: "lb2", mediaId: "B 正确视频" }]),
        );
      return original(url, options);
    }),
  );
  render(<App />);
  await screen.findByRole("option", { name: "小林" });
  fireEvent.click(screen.getByRole("button", { name: "人物与素材" }));
  fireEvent.click(screen.getByRole("button", { name: /小林 音色/ }));
  fireEvent.click(screen.getByRole("button", { name: /小夏 音色/ }));
  await screen.findByRole("button", { name: "B 形象一" });
  resolveLooks(
    response([{ id: "la", avatarId: "a", name: "A 过期形象", imageId: "img" }]),
  );
  await new Promise((r) => setTimeout(r, 0));
  expect(
    screen.queryByRole("button", { name: "A 过期形象" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "B 形象一" }));
  fireEvent.click(screen.getByRole("button", { name: "B 形象二" }));
  await screen.findByText("B 正确视频");
  resolveIdles(response([{ id: "ia", lookId: "lb1", mediaId: "A 过期视频" }]));
  await new Promise((r) => setTimeout(r, 0));
  expect(screen.queryByText("A 过期视频")).not.toBeInTheDocument();
  expect(screen.getByText("B 正确视频")).toBeInTheDocument();
});
it.each(["host", "stop"])(
  "rejects delayed pre-%s poll response",
  async (action) => {
    let tick!: () => void;
    const realInterval = globalThis.setInterval;
    vi.spyOn(globalThis, "setInterval").mockImplementation(((
      fn: () => void,
      ms: number,
    ) => {
      if (ms === 5000) {
        tick = fn;
        return 1;
      }
      return realInterval(fn, ms);
    }) as any);
    const original = globalThis.fetch;
    let resolvePoll!: (v: any) => void;
    let gets = 0;
    const old = {
      id: "s",
      avatarId: "a",
      lookId: "la",
      platform: "douyin",
      status: "running",
      hostEnabled: true,
    };
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string, options: any = {}) => {
        if (url === "/api/live" && options.method === "GET") {
          gets++;
          if (gets === 2)
            return new Promise((r) => {
              resolvePoll = r;
            });
        }
        return original(url, options);
      }),
    );
    render(<App />);
    fireEvent.change(await screen.findByLabelText("直播人物"), {
      target: { value: "a" },
    });
    await screen.findByRole("option", { name: "春装" });
    fireEvent.change(screen.getByLabelText("直播形象"), {
      target: { value: "la" },
    });
    fireEvent.click(screen.getByRole("button", { name: "开始直播" }));
    await screen.findByRole("button", { name: "结束直播" });
    tick();
    await waitFor(() => expect(resolvePoll).toBeDefined());
    fireEvent.click(
      screen.getByRole("button", {
        name: action === "host" ? "暂停主持" : "结束直播",
      }),
    );
    await waitFor(() =>
      expect(calls.some((c) => c.url.endsWith("/" + action))).toBe(true),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("button", {
          name: action === "host" ? "恢复主持" : "开始直播",
        }),
      ).toBeEnabled(),
    );
    resolvePoll({
      ok: true,
      status: 200,
      text: async () => JSON.stringify({ session: old }),
    });
    await new Promise((r) => setTimeout(r, 0));
    expect(
      screen.getByRole("button", {
        name: action === "host" ? "恢复主持" : "开始直播",
      }),
    ).toBeInTheDocument();
    if (action === "stop")
      expect(screen.getByLabelText("本场采集令牌")).toHaveValue("");
  },
);
it("clears collector and relay state when polling discovers a different session", async () => {
  let tick!: () => void;
  const realInterval = globalThis.setInterval;
  vi.spyOn(globalThis, "setInterval").mockImplementation(((
    fn: () => void,
    ms: number,
  ) => {
    if (ms === 5000) {
      tick = fn;
      return 1;
    }
    return realInterval(fn, ms);
  }) as any);
  const original = globalThis.fetch;
  let gets = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, options: any = {}) => {
      if (url === "/api/live" && options.method === "GET" && ++gets > 1)
        return Promise.resolve({
          ok: true,
          status: 200,
          text: async () =>
            JSON.stringify({
              session: {
                id: "new-session",
                avatarId: "b",
                lookId: "lb",
                platform: "xiaohongshu",
                status: "running",
                hostEnabled: false,
              },
            }),
        });
      return original(url, options);
    }),
  );
  render(<App />);
  fireEvent.change(await screen.findByLabelText("直播人物"), {
    target: { value: "a" },
  });
  await screen.findByRole("option", { name: "春装" });
  fireEvent.change(screen.getByLabelText("直播形象"), {
    target: { value: "la" },
  });
  fireEvent.click(screen.getByRole("button", { name: "开始直播" }));
  await screen.findByRole("button", { name: "结束直播" });
  expect(screen.getByLabelText("本场采集令牌")).toHaveValue("token");
  fireEvent.click(screen.getByText("平台转推"));
  fireEvent.change(screen.getByLabelText("RTMP / RTMPS 服务地址"), {
    target: { value: "rtmps://example.invalid/live" },
  });
  fireEvent.change(screen.getByLabelText("推流密钥"), {
    target: { value: "test-key" },
  });
  fireEvent.click(screen.getByRole("button", { name: "启用转推" }));
  await screen.findByRole("button", { name: "停止转推" });
  tick();
  await screen.findByText("new-session");
  expect(screen.getByLabelText("本场采集令牌")).toHaveValue("");
  expect(screen.getByLabelText("推流密钥")).toHaveValue("");
  expect(
    screen.queryByRole("button", { name: "停止转推" }),
  ).not.toBeInTheDocument();
});
it("serializes slow background polls and accepts their results", async () => {
  let tick!: () => void;
  const realInterval = globalThis.setInterval;
  vi.spyOn(globalThis, "setInterval").mockImplementation(((
    fn: () => void,
    ms: number,
  ) => {
    if (ms === 5000) {
      tick = fn;
      return 1;
    }
    return realInterval(fn, ms);
  }) as any);
  const original = globalThis.fetch;
  const replies: Array<(value: any) => void> = [];
  let gets = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, options: any = {}) => {
      if (url === "/api/live" && options.method === "GET" && ++gets > 1)
        return new Promise((resolve) => replies.push(resolve));
      return original(url, options);
    }),
  );
  render(<App />);
  await screen.findByRole("option", { name: "小林" });
  tick();
  await waitFor(() => expect(replies).toHaveLength(1));
  tick();
  tick();
  expect(gets).toBe(2);
  replies[0]({
    ok: true,
    status: 200,
    text: async () =>
      JSON.stringify({
        session: {
          id: "slow-session",
          avatarId: "a",
          lookId: "la",
          platform: "douyin",
          status: "running",
          hostEnabled: false,
        },
      }),
  });
  await screen.findByText("slow-session");
  expect(screen.getByRole("button", { name: "结束直播" })).toBeEnabled();
  tick();
  await waitFor(() => expect(replies).toHaveLength(2));
  replies[1]({
    ok: true,
    status: 200,
    text: async () => JSON.stringify({ session: null }),
  });
  await screen.findByRole("button", { name: "开始直播" });
});

it("shows relay failure and explicit disable after refreshing a live session", async () => {
  const original = globalThis.fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, options: any = {}) => {
      if (url === "/api/live" && options.method === "GET")
        return Promise.resolve({
          ok: true,
          status: 200,
          text: async () =>
            JSON.stringify({
              session: {
                id: "s",
                status: "live",
                avatarId: "a",
                lookId: "la",
                platform: "douyin",
                relayState: "failed",
              },
            }),
        });
      return original(url, options);
    }),
  );
  render(<App />);
  await screen.findByText("转推进程已退出");
  expect(screen.getByRole("button", { name: "结束直播" })).toBeEnabled();
  fireEvent.click(screen.getByText("平台转推"));
  fireEvent.click(screen.getByRole("button", { name: "确保停止转推" }));
  await waitFor(() =>
    expect(
      calls.some((c) => c.url.endsWith("/relay") && c.body.enabled === false),
    ).toBe(true),
  );
});

it("displays the separate results of the worker and model connection check", async () => {
  const original = globalThis.fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn((path, options) => {
      if (path === "/api/settings/check") {
        return Promise.resolve(
          new Response(JSON.stringify({ worker: false, llm: true })),
        );
      }
      return original(path, options);
    }),
  );
  render(<App />);
  await screen.findByRole("option", { name: "小林" });
  fireEvent.click(screen.getByRole("button", { name: "服务设置" }));
  fireEvent.click(screen.getByRole("button", { name: "检查服务连接" }));
  expect(await screen.findByText("未就绪，请检查 Worker 服务")).toBeVisible();
  expect(screen.getByText("连接正常")).toBeVisible();
});

it("explains a failed startup and takes the operator to service checks", async () => {
  const original = globalThis.fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn((path, options) => {
      if (path === "/api/live" && options?.method === "POST") {
        return Promise.resolve(
          new Response(
            JSON.stringify({ error: "Worker start failed or cancelled" }),
            { status: 503 },
          ),
        );
      }
      return original(path, options);
    }),
  );
  render(<App />);
  fireEvent.change(await screen.findByLabelText("直播人物"), {
    target: { value: "a" },
  });
  await screen.findByRole("option", { name: "春装" });
  fireEvent.change(screen.getByLabelText("直播形象"), {
    target: { value: "la" },
  });
  fireEvent.click(screen.getByRole("button", { name: "开始直播" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "直播未能启动。请到服务设置检查直播处理服务",
  );
  fireEvent.click(screen.getByRole("button", { name: "前往服务设置" }));
  expect(screen.getByRole("button", { name: "检查服务连接" })).toBeVisible();
});

it("opens the selected look's idle uploads directly from broadcast preparation", async () => {
  const original = globalThis.fetch;
  let resolveMetadata!: (media: { url: string }) => void;
  const delayedMetadata = new Promise<{ url: string }>((resolve) => {
    resolveMetadata = resolve;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn((path, options) => {
      if (path === "/api/media/img")
        return delayedMetadata.then((m) => new Response(JSON.stringify(m)));
      if (path === "/api/looks/la/idles")
        return Promise.resolve(
          new Response(
            JSON.stringify([{ id: "i", lookId: "la", mediaId: "现有底片" }]),
          ),
        );
      return original(path, options);
    }),
  );
  render(<App />);
  fireEvent.change(await screen.findByLabelText("直播人物"), {
    target: { value: "a" },
  });
  await screen.findByRole("option", { name: "春装" });
  fireEvent.change(screen.getByLabelText("直播形象"), {
    target: { value: "la" },
  });
  fireEvent.click(screen.getByRole("button", { name: "上传 / 更换底片" }));
  expect(await screen.findByText("现有底片")).toBeVisible();
  expect(screen.getByLabelText("人物名称")).toHaveValue("小林");
  expect(screen.getByLabelText("形象名称")).toHaveValue("春装");
  expect(
    screen.getByRole("region", { name: "口型底片与待机视频" }),
  ).toHaveFocus();
  expect(screen.getByLabelText(/上传底片 \/ 待机视频/)).toHaveAttribute(
    "type",
    "file",
  );
  expect(screen.getByRole("button", { name: "默认底片" })).toBeDisabled();
  const previewFrame = screen.getByRole("figure", { name: "形象图预览" });
  expect(previewFrame).toHaveClass("material-image-frame");
  expect(screen.queryByAltText("已上传的形象图")).toBeNull();
  resolveMetadata({ url: "/media/portrait.png" });
  expect(previewFrame).toContainElement(
    await screen.findByAltText("已上传的形象图"),
  );
  expect(
    screen.getByRole("region", { name: "口型底片与待机视频" }),
  ).toHaveFocus();
});
it("defaults new avatars to companion and limits live avatar edits to persona", async () => {
  const original = globalThis.fetch;
  vi.stubGlobal("fetch", async (url: string, options: RequestInit) => {
    if (url === "/api/live")
      return new Response(
        JSON.stringify({
          session: {
            id: "s",
            avatarId: "a",
            lookId: "la",
            platform: "douyin",
            status: "live",
            hostEnabled: true,
            hostMode: "companion",
          },
        }),
      );
    if (url === "/api/avatars/a" && options.method === "PATCH") {
      calls.push({ url, body: JSON.parse(String(options.body)) });
      return new Response(JSON.stringify({ id: "a" }));
    }
    return original(url, options);
  });
  render(<App />);
  const liveMode = await screen.findByLabelText("本场主持模式");
  await waitFor(() => expect(liveMode).toHaveValue("companion"));
  expect(liveMode).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "人物与素材" }));
  expect(screen.getByLabelText("默认主持模式")).toHaveValue("companion");
  fireEvent.click(await screen.findByRole("button", { name: /小林/ }));
  expect(screen.getByLabelText("默认主持模式")).toHaveValue("fixed");
  expect(screen.getByLabelText("默认主持模式")).toBeDisabled();
  expect(screen.getByLabelText("人物名称")).toBeDisabled();
  expect(screen.getByLabelText("音色 ID")).toBeDisabled();
  fireEvent.change(screen.getByLabelText("人物人设"), {
    target: { value: "喜欢花草" },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存人物" }));
  await waitFor(() =>
    expect(calls).toContainEqual({
      url: "/api/avatars/a",
      body: { persona: "喜欢花草" },
    }),
  );
});
it("keeps fixed-session controls without requesting companion status", async () => {
  const original = globalThis.fetch;
  vi.stubGlobal("fetch", async (url: string, options: RequestInit) => {
    if (url === "/api/live")
      return new Response(
        JSON.stringify({
          session: {
            id: "fixed",
            avatarId: "a",
            lookId: "la",
            platform: "douyin",
            status: "live",
            hostEnabled: true,
            hostMode: "fixed",
          },
        }),
      );
    return original(url, options);
  });
  render(<App />);
  expect(await screen.findByRole("button", { name: "暂停主持" })).toBeEnabled();
  expect(screen.getByLabelText("手动口播内容")).toBeEnabled();
  expect(screen.queryByText("主持状态")).toBeNull();
  expect(calls.some((call) => call.url.endsWith("/host-status"))).toBe(false);
});
