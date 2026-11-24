import { useState } from "react";
import { api, ApiError } from "../api";
import { hostError, useHostRead } from "./useHostRead";

type Topic = {
  id: string;
  title: string;
  state: string;
  angles: { text: string }[];
};
type Topics = {
  topics: Topic[];
  suggestedInterests: string[] | null;
  generation: { state: string; added?: number };
};
type Props = {
  avatarId: string;
  interests?: string[];
  onSaved?: () => Promise<void>;
};
const lines = (value: string) =>
  value
    .split("\n")
    .map((v) => v.trim())
    .filter(Boolean);
const states: Record<string, string> = {
  ready: "待使用",
  active: "进行中",
  done: "已完成",
  disabled: "已禁用",
  quarantined: "已隔离",
};

export default function HostContentManager(props: Props) {
  return <Content key={props.avatarId} {...props} />;
}
function Content(props: Props) {
  const path = `/avatars/${props.avatarId}/host-topics`;
  const read = useHostRead<Topics>(path);
  if (!read.data)
    return (
      <div className="host-content">
        <h3>陪伴主持主题</h3>
        {read.error ? (
          <div role="alert">
            {read.error} <button onClick={read.refresh}>重新读取</button>
          </div>
        ) : (
          <p>正在读取主题…</p>
        )}
      </div>
    );
  return <TopicEditor {...props} {...read} path={path} data={read.data} />;
}
function TopicEditor({
  avatarId,
  interests,
  onSaved,
  path,
  data,
  error,
  refresh,
  reportError,
}: Props & {
  path: string;
  data: Topics;
  error: string;
  refresh: () => void;
  reportError: (error: unknown) => void;
}) {
  const [draft, setDraft] = useState((interests || []).join("\n"));
  const [saved, setSaved] = useState(draft);
  const [edit, setEdit] = useState("");
  const [title, setTitle] = useState("");
  const [angles, setAngles] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  async function change(action: () => Promise<void>) {
    setBusy(true);
    setFailure("");
    try {
      await action();
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) reportError(e);
      else setFailure(hostError(e));
    } finally {
      setBusy(false);
      refresh();
    }
  }
  const items = lines(draft);
  const viewpoints = lines(angles);
  const job = data?.generation;
  const pending = job?.state === "running";
  return (
    <fieldset className="host-content" disabled={busy}>
      <h3>陪伴主持主题</h3>
      <label>
        兴趣词（每行一个，最多五个）
        <textarea
          rows={3}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
      </label>
      <div className="actions">
        <button
          disabled={
            saved === draft ||
            items.length > 5 ||
            items.some((s) => Array.from(s).length > 40)
          }
          onClick={() =>
            change(async () => {
              await api(`/avatars/${avatarId}`, "PATCH", { interests: items });
              setSaved(draft);
              await onSaved?.();
            })
          }
        >
          保存兴趣
        </button>
      </div>
      {Boolean(data?.suggestedInterests?.length) && (
        <p>
          建议兴趣 · {data!.suggestedInterests!.join("、")}{" "}
          <button
            onClick={() => setDraft(data!.suggestedInterests!.join("\n"))}
          >
            采用建议兴趣
          </button>
        </p>
      )}
      <p className="hint">陪伴主持会自动补充主题。修改主题会影响后续备稿。</p>
      {(failure || error) && (
        <div role="alert">
          {failure || error}{" "}
          <button
            onClick={() => {
              setFailure("");
              refresh();
            }}
          >
            重新读取
          </button>
        </div>
      )}
      <div className="actions">
        <button
          disabled={pending}
          onClick={() =>
            change(async () => {
              await api(path + "/generate", "POST");
            })
          }
        >
          {pending
            ? "主题生成中…"
            : job?.state === "failed"
              ? "重试生成"
              : "生成主题"}
        </button>
      </div>
      {job?.state === "failed" && !pending && (
        <p role="status">主题生成未完成，请稍后重试。</p>
      )}
      {job?.state === "completed" && !pending && (
        <p className="hint">本次已新增 {job.added ?? 0} 个主题。</p>
      )}
      {data?.topics?.length === 0 && (
        <p className="empty">暂无主题，可生成第一批主题。</p>
      )}
      {data?.topics?.map((t) => (
        <article className="script" key={t.id}>
          <strong>{t.title}</strong>{" "}
          <small>{states[t.state] || "状态未知"}</small>
          {edit === t.id ? (
            <>
              <label>
                主题标题
                <input
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                />
              </label>
              <label>
                话题角度（每行一个，三至五个）
                <textarea
                  value={angles}
                  onChange={(e) => setAngles(e.target.value)}
                />
              </label>
              <div className="actions">
                <button
                  disabled={
                    !title.trim() ||
                    Array.from(title.trim()).length > 80 ||
                    viewpoints.length < 3 ||
                    viewpoints.length > 5 ||
                    viewpoints.some((s) => Array.from(s).length > 200) ||
                    new Set(viewpoints).size !== viewpoints.length
                  }
                  onClick={() =>
                    change(async () => {
                      await api(`/host-topics/${t.id}`, "PATCH", {
                        title: title.trim(),
                        angles: viewpoints,
                      });
                      setEdit("");
                    })
                  }
                >
                  保存主题
                </button>
                <button onClick={() => setEdit("")}>取消编辑</button>
              </div>
            </>
          ) : (
            <>
              <ul>
                {t.angles.map((a) => (
                  <li key={a.text}>{a.text}</li>
                ))}
              </ul>
              {t.state !== "disabled" && (
                <div className="actions">
                  <button
                    onClick={() => {
                      setEdit(t.id);
                      setTitle(t.title);
                      setAngles(t.angles.map((a) => a.text).join("\n"));
                    }}
                  >
                    编辑主题
                  </button>
                  <button
                    onClick={() =>
                      change(async () => {
                        await api(`/host-topics/${t.id}`, "PATCH", {
                          state: "disabled",
                        });
                      })
                    }
                  >
                    禁用主题
                  </button>
                </div>
              )}
            </>
          )}
        </article>
      ))}
    </fieldset>
  );
}
