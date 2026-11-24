import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import HostContentManager from "./HostContentManager";
import HostStatus from "./HostStatus";
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
const json = (data: unknown, status = 200) =>
  new Response(JSON.stringify(data), { status });
const wrapper = (title: string, state = "idle") => ({
  topics: title
    ? [
        {
          id: "t",
          title,
          state: "ready",
          angles: ["一", "二", "三"].map((text) => ({ text })),
        },
      ]
    : [],
  suggestedInterests: ["园艺"],
  generation: { state },
});
it("discards late topic and session reads after selection changes", async () => {
  const late: ((r: Response) => void)[] = [];
  vi.stubGlobal("fetch", (url: string) =>
    url.includes("/old/")
      ? new Promise<Response>((r) => late.push(r))
      : Promise.resolve(
          json(
            url.endsWith("host-status")
              ? { state: "paused", topicTitle: "当前场次" }
              : wrapper("当前主题"),
          ),
        ),
  );
  const view = render(
    <>
      <HostContentManager avatarId="old" />
      <HostStatus sessionId="old" />
    </>,
  );
  view.rerender(
    <>
      <HostContentManager avatarId="new" />
      <HostStatus sessionId="new" />
    </>,
  );
  await screen.findByText("当前主题");
  await screen.findByText("当前场次");
  await act(async () => {
    late[0](json(wrapper("过期主题")));
    late[1](json({ state: "waiting", topicTitle: "过期场次" }));
  });
  expect(screen.queryByText(/过期/)).toBeNull();
});
it.each([
  [401, "管理员令牌"],
  [409, "状态已变化"],
  [503, "服务暂不可用"],
] as const)("explains %i without leaking details", async (status, label) => {
  vi.stubGlobal("fetch", () =>
    Promise.resolve(json({ error: "secret-internal" }, status)),
  );
  render(<HostContentManager avatarId="a" />);
  expect(await screen.findByRole("alert")).toHaveTextContent(label);
  expect(screen.queryByText(/secret-internal/)).toBeNull();
});
it("lets an operator edit a suggestion before saving and disable a topic", async () => {
  const fetchMock = vi.fn(async (_url: string, options: RequestInit) =>
    json(options.method === "PATCH" ? {} : wrapper("盆栽")),
  );
  vi.stubGlobal("fetch", fetchMock);
  render(<HostContentManager avatarId="a" />);
  fireEvent.click(await screen.findByRole("button", { name: "采用建议兴趣" }));
  fireEvent.change(screen.getByLabelText(/兴趣词/), {
    target: { value: "多肉" },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存兴趣" }));
  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/avatars/a",
      expect.objectContaining({
        body: JSON.stringify({ interests: ["多肉"] }),
      }),
    ),
  );
  fireEvent.click(await screen.findByRole("button", { name: "禁用主题" }));
  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/host-topics/t",
      expect.objectContaining({ body: JSON.stringify({ state: "disabled" }) }),
    ),
  );
});
it("reads job state after acceptance and recovers from a failed generation", async () => {
  let status = "failed";
  vi.stubGlobal("fetch", async (url: string) => {
    if (url.endsWith("/generate")) {
      status = "running";
      return json({ jobId: "j" }, 202);
    }
    return json(wrapper(status === "completed" ? "新主题" : "", status));
  });
  render(<HostContentManager avatarId="a" />);
  fireEvent.click(await screen.findByRole("button", { name: "重试生成" }));
  expect(
    await screen.findByRole("button", { name: "主题生成中…" }),
  ).toBeDisabled();
  status = "completed";
  expect(
    await screen.findByText("新主题", {}, { timeout: 4500 }),
  ).toBeInTheDocument();
});
it("edits a topic using three plain-text angles", async () => {
  const fetchMock = vi.fn(async () => json(wrapper("盆栽")));
  vi.stubGlobal("fetch", fetchMock);
  render(<HostContentManager avatarId="a" />);
  fireEvent.click(await screen.findByRole("button", { name: "编辑主题" }));
  fireEvent.change(screen.getByLabelText("主题标题"), {
    target: { value: "院子" },
  });
  fireEvent.change(screen.getByLabelText(/话题角度/), {
    target: { value: "绿植\n光照\n土壤" },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存主题" }));
  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/host-topics/t",
      expect.objectContaining({
        body: JSON.stringify({
          title: "院子",
          angles: ["绿植", "光照", "土壤"],
        }),
      }),
    ),
  );
});
it("withdraws protected topics and controls when a later read loses authorization", async () => {
  let revoked = false;
  vi.stubGlobal("fetch", () =>
    Promise.resolve(revoked ? json({}, 401) : json(wrapper("私人主题"))),
  );
  render(<HostContentManager avatarId="a" interests={["私人兴趣"]} />);
  await screen.findByText("私人主题");
  fireEvent.click(screen.getByRole("button", { name: "编辑主题" }));
  revoked = true;
  await screen.findByRole("alert", {}, { timeout: 4500 });
  expect(screen.queryByText("私人主题")).toBeNull();
  expect(screen.queryByDisplayValue("私人兴趣")).toBeNull();
  expect(screen.queryByRole("button", { name: "保存主题" })).toBeNull();
  expect(screen.queryByRole("button", { name: "生成主题" })).toBeNull();
});
