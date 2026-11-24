import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { api, upload, tokenKey } from "./api";
import type {
  Avatar,
  Look,
  Idle,
  Script,
  ScriptKind,
  Session,
  Settings,
  Platform,
} from "./types";
import Preview from "./components/Preview";
const kinds: Record<ScriptKind, string> = {
  host: "自动主持",
  welcome: "欢迎观众",
  gift: "礼物感谢",
  like: "点赞感谢",
  comment: "评论回复",
};
const active = (s: Session | null) =>
  !!s && !["stopped", "failed", "error"].includes(s.status);
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
    </label>
  );
}
export default function App() {
  const [token, setToken] = useState(
    (sessionStorage.getItem(tokenKey) || "").trim(),
  );
  const [connected, setConnected] = useState(!!token);
  const [page, setPage] = useState("studio");
  const [avatars, setAvatars] = useState<Avatar[]>([]);
  const [avatarId, setAvatarId] = useState("");
  const [looks, setLooks] = useState<Look[]>([]);
  const [lookId, setLookId] = useState("");
  const [session, setSession] = useState<Session | null>(null);
  const [platform, setPlatform] = useState<Platform>("douyin");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [collector, setCollector] = useState("");
  const [speech, setSpeech] = useState("");
  const [relayServer, setRelayServer] = useState("");
  const [relayKey, setRelayKey] = useState("");
  const [settings, setSettings] = useState<Settings | null>(null);
  const running = active(session);
  // A read may finish after a command; only the current generation can update live state.
  const liveVersion = useRef(0);
  const liveReads = useRef(0);
  // Count overlapping commands so polling and selection locks resume only after all settle.
  const pending = useRef(0);
  const sessionIdRef = useRef("");
  const [collectorSession, setCollectorSession] = useState("");
  const relayState = session?.relayState || "disabled";
  const relayOn = relayState === "running" || relayState === "launching";
  const relayDescription = {
    disabled: "转推已关闭", launching: "正在启动转推", running: "转推进程运行中",
    failed: "转推进程已退出", unknown: "转推状态暂时未知",
  }[relayState];
  const acceptSession = useCallback((next: Session | null) => {
    if (sessionIdRef.current !== (next?.id || "") || !active(next)) {
      setCollector("");
      setCollectorSession("");
      setRelayKey("");
      setRelayServer("");
    }
    sessionIdRef.current = next?.id || "";
    setSession(next);
  }, []);
  const readLive = useCallback(async () => {
    const version = ++liveVersion.current;
    liveReads.current++;
    try {
      const result = await api<{ session: Session | null }>("/live");
      if (version === liveVersion.current) acceptSession(result.session);
    } catch (e) {
      if (version === liveVersion.current) throw e;
    } finally {
      liveReads.current--;
    }
  }, [acceptSession]);
  const refresh = useCallback(async () => {
    await Promise.all([api<Avatar[]>("/avatars").then(setAvatars), readLive()]);
  }, [readLive]);
  const act = async (fn: () => Promise<void>) => {
    setError("");
    setNotice("");
    setBusy(true);
    pending.current++;
    liveVersion.current++;
    try {
      await fn();
    } catch (e) {
      setError(e instanceof Error ? e.message : "操作失败，请重试");
    } finally {
      liveVersion.current++;
      pending.current--;
      setBusy(pending.current > 0);
    }
  };
  useEffect(() => {
    if (!connected) return;
    refresh().catch((e) => setError(e.message));
    const id = setInterval(() => {
      if (!pending.current && !liveReads.current)
        readLive().catch((e) => setError(e.message));
    }, 5000);
    return () => {
      liveVersion.current++;
      clearInterval(id);
    };
  }, [connected, refresh, readLive]);
  useEffect(() => {
    setLooks([]);
    setLookId("");
    if (!avatarId) return;
    let cancelled = false;
    api<Look[]>(`/avatars/${avatarId}/looks`)
      .then((v) => {
        if (!cancelled) setLooks(v.filter((l) => l.avatarId === avatarId));
      })
      .catch((e) => setError(e.message));
    return () => {
      cancelled = true;
    };
  }, [avatarId]);
  if (!connected)
    return (
      <div className="login">
        <div className="login-panel">
          <div className="brand">
            LingCast<span>直播工作台</span>
          </div>
          <h1>连接你的直播空间</h1>
          <p>输入部署时设置的管理员令牌。令牌仅保留在当前浏览器会话。</p>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              sessionStorage.setItem(tokenKey, token.trim());
              setToken(token.trim());
              setConnected(true);
            }}
          >
            <Field label="管理员令牌">
              <input
                type="password"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                autoComplete="off"
                required
              />
            </Field>
            <button className="primary" disabled={!token.trim()}>
              连接工作台
            </button>
          </form>
        </div>
      </div>
    );
  return (
    <div className="shell">
      <aside>
        <div className="brand">
          LingCast<span>直播工作台</span>
        </div>
        <nav aria-label="主导航">
          {[
            ["studio", "直播控制"],
            ["avatars", "人物与素材"],
            ["scripts", "话术库"],
            ["settings", "服务设置"],
          ].map(([key, label]) => (
            <button
              key={key}
              disabled={busy}
              aria-current={page === key ? "page" : undefined}
              className={page === key ? "selected" : ""}
              onClick={() => setPage(key)}
            >
              {label}
            </button>
          ))}
        </nav>
        <div className="sidebar-foot">
          <span className={`dot ${running ? "on" : ""}`} />
          {running ? "直播进行中" : "尚未开播"}
          <button
            className="text"
            disabled={busy}
            onClick={() => {
              liveVersion.current++;
              sessionStorage.removeItem(tokenKey);
              setConnected(false);
              setToken("");
              setCollector("");
            }}
          >
            断开管理连接
          </button>
        </div>
      </aside>
      <main>
        <header>
          <div>
            <h1>
              {
                (
                  {
                    studio: "直播控制",
                    avatars: "人物与素材",
                    scripts: "话术库",
                    settings: "服务设置",
                  } as Record<string, string>
                )[page]
              }
            </h1>
            <p>
              {page === "studio"
                ? "一场直播，一个形象。准备就绪，再与观众见面。"
                : "为每一场直播做好准备。"}
            </p>
          </div>
          <button onClick={() => act(refresh)} disabled={busy}>
            刷新状态
          </button>
        </header>
        {error && (
          <div role="alert" className="alert">
            {error}
            <button className="text" onClick={() => setError("")}>
              关闭
            </button>
          </div>
        )}
        {notice && (
          <div role="status" className="notice">
            {notice}
          </div>
        )}
        {page === "studio" && (
          <>
            <div className="studio-grid">
              <section className="screen">
                <div className="section-head">
                  <h2>节目预览</h2>
                  <span className={`status ${running ? "live" : ""}`}>
                    {session?.status || "未开播"}
                  </span>
                </div>
                <Preview
                  url={
                    running && session
                      ? `/api/live/${session.id}/preview?token=${encodeURIComponent(token)}`
                      : ""
                  }
                />
                <div className="screen-footer">
                  {running && session ? (
                    <>
                      <span>
                        {avatars.find((a) => a.id === session.avatarId)?.name ||
                          "当前人物"}{" "}
                        · {session.platform === "douyin" ? "抖音" : "小红书"}
                      </span>
                      <span>本场形象已锁定</span>
                    </>
                  ) : (
                    <span>开播后提供实时画面预览</span>
                  )}
                </div>
                {session?.error && (
                  <p role="alert" className="alert">
                    {session.error}
                  </p>
                )}
                {session && !running && session.status !== "stopped" && (
                  <button
                    disabled={busy}
                    onClick={() =>
                      act(async () => {
                        await api(`/live/${session.id}/stop`, "POST");
                        await refresh();
                      })
                    }
                  >
                    清理故障场次
                  </button>
                )}
              </section>
              <section className="panel">
                <h2>开播准备</h2>
                <Field label="直播人物">
                  <select
                    aria-label="直播人物"
                    value={avatarId}
                    disabled={running || busy}
                    onChange={(e) => setAvatarId(e.target.value)}
                  >
                    <option value="">请选择人物</option>
                    {avatars.map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.name}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field label="直播形象">
                  <select
                    aria-label="直播形象"
                    value={lookId}
                    disabled={running || busy || !avatarId}
                    onChange={(e) => setLookId(e.target.value)}
                  >
                    <option value="">请选择形象</option>
                    {looks.map((l) => (
                      <option value={l.id} key={l.id}>
                        {l.name}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field label="直播平台">
                  <select
                    aria-label="直播平台"
                    value={platform}
                    disabled={running || busy}
                    onChange={(e) => setPlatform(e.target.value as Platform)}
                  >
                    <option value="douyin">抖音</option>
                    <option value="xiaohongshu">小红书</option>
                  </select>
                </Field>
                <p className="hint">
                  形象需包含已验证的静音 idle 视频，并指定默认口型底片。
                </p>
                {running ? (
                  <button
                    className="danger full"
                    disabled={busy}
                    onClick={() =>
                      act(async () => {
                        await api(`/live/${session!.id}/stop`, "POST");
                        setCollector("");
                        await refresh();
                      })
                    }
                  >
                    结束直播
                  </button>
                ) : (
                  <button
                    className="primary full"
                    disabled={
                      busy ||
                      !avatarId ||
                      !lookId ||
                      !looks.find((l) => l.id === lookId)?.defaultIdleId
                    }
                    onClick={() =>
                      act(async () => {
                        const r = await api<{
                          session: Session;
                          collectorToken: string;
                        }>("/live", "POST", { avatarId, lookId, platform });
                        acceptSession(r.session);
                        setCollector(r.collectorToken);
                        setCollectorSession(r.session.id);
                      })
                    }
                  >
                    {busy ? "处理中…" : "开始直播"}
                  </button>
                )}
              </section>
            </div>
            <div className="two-col">
              <section className="panel">
                <div className="section-head">
                  <h2>口播与主持</h2>
                  <button
                    disabled={!running || busy}
                    onClick={() =>
                      act(async () => {
                        await api(`/live/${session!.id}/host`, "PATCH", {
                          enabled: !session!.hostEnabled,
                        });
                        setSession({
                          ...session!,
                          hostEnabled: !session!.hostEnabled,
                        });
                      })
                    }
                  >
                    {session?.hostEnabled ? "暂停主持" : "恢复主持"}
                  </button>
                </div>
                <p className="hint">
                  手动稿按原文播放，在当前短段结束后优先安排。
                </p>
                <Field label="手动口播内容">
                  <textarea
                    value={speech}
                    onChange={(e) => setSpeech(e.target.value)}
                    placeholder="输入这次要说的话…"
                    rows={4}
                  />
                </Field>
                <button
                  disabled={!running || busy || !speech.trim()}
                  onClick={() =>
                    act(async () => {
                      await api(`/live/${session!.id}/speak`, "POST", {
                        text: speech,
                      });
                      setSpeech("");
                      setNotice("口播已加入本场队列");
                    })
                  }
                >
                  加入口播队列
                </button>
              </section>
              <section className="panel">
                <h2>观众事件采集</h2>
                <p className="hint">
                  在 Chrome
                  采集插件中填写以下信息，再主动开始采集。令牌仅在创建或轮换时显示。
                </p>
                <dl className="details">
                  <dt>业务地址</dt>
                  <dd>{location.origin}</dd>
                  <dt>场次 ID</dt>
                  <dd>{session?.id || "开播后生成"}</dd>
                  <dt>平台</dt>
                  <dd>
                    {session
                      ? session.platform === "douyin"
                        ? "抖音"
                        : "小红书"
                      : "开播后确定"}
                  </dd>
                </dl>
                <Field label="本场采集令牌">
                  <input
                    readOnly
                    type="password"
                    value={collectorSession === session?.id ? collector : ""}
                    placeholder="未显示，可轮换生成新令牌"
                  />
                </Field>
                <div className="actions">
                  <button
                    disabled={!running || busy}
                    onClick={() =>
                      act(async () => {
                        const r = await api<{ collectorToken: string }>(
                          `/live/${session!.id}/collector-token`,
                          "POST",
                        );
                        if (sessionIdRef.current !== session!.id) return;
                        setCollector(r.collectorToken);
                        setCollectorSession(session!.id);
                        setNotice("新令牌已生成，旧令牌已失效");
                      })
                    }
                  >
                    轮换采集令牌
                  </button>
                  <button
                    disabled={!collector || collectorSession !== session?.id}
                    onClick={() =>
                      act(async () => {
                        await navigator.clipboard.writeText(collector);
                        setNotice("采集令牌已复制");
                      })
                    }
                  >
                    复制令牌
                  </button>
                </div>
              </section>
            </div>
            <section className="panel relay">
              <h2>平台转推</h2>
              <p role="status">{relayDescription}</p>
              <p className="hint">
                选择平台不会开启外部推流。请填写平台提供的地址与密钥，再显式启用。
              </p>
              <div className="relay-fields">
                <Field label="RTMP / RTMPS 服务地址">
                  <input
                    value={relayServer}
                    placeholder="rtmps://…"
                    onChange={(e) => setRelayServer(e.target.value)}
                  />
                </Field>
                <Field label="推流密钥">
                  <input
                    type="password"
                    autoComplete="off"
                    value={relayKey}
                    onChange={(e) => setRelayKey(e.target.value)}
                  />
                </Field>
                <button
                  disabled={
                    !running ||
                    busy ||
                    (!relayOn &&
                      (!/^rtmps?:\/\//.test(relayServer) || !relayKey))
                  }
                  onClick={() =>
                    act(async () => {
                      await api(`/live/${session!.id}/relay`, "PUT", {
                        enabled: !relayOn,
                        server: relayServer,
                        streamKey: relayKey,
                      });
                      if (sessionIdRef.current !== session!.id) return;
                      setSession({ ...session!, relayState: relayOn ? "disabled" : "launching" });
                    })
                  }
                >
                  {relayOn ? "停止转推" : "启用转推"}
                </button>
                <button
                  disabled={!running || busy}
                  onClick={() =>
                    act(async () => {
                      await api(`/live/${session!.id}/relay`, "PUT", {
                        enabled: false,
                        server: "",
                        streamKey: "",
                      });
                      if (sessionIdRef.current === session!.id)
                        setSession({ ...session!, relayState: "disabled" });
                    })
                  }
                >
                  确保停止转推
                </button>
              </div>
            </section>
          </>
        )}
        {page === "avatars" && (
          <AvatarManager
            avatars={avatars}
            refresh={refresh}
            act={act}
            busy={busy}
            lockedAvatar={running ? session!.avatarId : ""}
          />
        )}
        {page === "scripts" && (
          <ScriptManager avatars={avatars} act={act} busy={busy} />
        )}
        {page === "settings" && (
          <section className="panel settings">
            <h2>服务连接</h2>
            <p className="hint">
              配置由部署环境提供，此处仅查看安全信息并检查连接。
            </p>
            <div className="actions">
              <button
                onClick={() =>
                  act(async () => setSettings(await api<Settings>("/settings")))
                }
                disabled={busy}
              >
                读取配置
              </button>
              <button
                onClick={() =>
                  act(async () => {
                    const result = await api<unknown>(
                      "/settings/check",
                      "POST",
                    );
                    setNotice(`连接检查结果：${JSON.stringify(result)}`);
                  })
                }
                disabled={busy}
              >
                检查服务连接
              </button>
            </div>
            {settings && (
              <dl className="details">
                <dt>业务时区</dt>
                <dd>{settings.timezone}</dd>
                <dt>Worker</dt>
                <dd>{settings.workerUrl || "未配置"}</dd>
                <dt>LLM</dt>
                <dd>{settings.llmUrl || "未配置"}</dd>
                <dt>语言模型</dt>
                <dd>{settings.llmModel || "未配置"}</dd>
                {Object.entries(settings.configured || {}).map(([k, v]) => (
                  <div className="config-line" key={k}>
                    <dt>{k}</dt>
                    <dd>{v ? "已配置" : "未配置"}</dd>
                  </div>
                ))}
              </dl>
            )}
          </section>
        )}
        <footer>LingCast Next · 素材由运营者提供 · 单场直播工作空间</footer>
      </main>
    </div>
  );
}
type Action = (fn: () => Promise<void>) => Promise<void>;
function AvatarManager({
  avatars,
  refresh,
  act,
  busy,
  lockedAvatar,
}: {
  avatars: Avatar[];
  refresh: () => Promise<void>;
  act: Action;
  busy: boolean;
  lockedAvatar: string;
}) {
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [persona, setPersona] = useState("");
  const [voiceId, setVoice] = useState("");
  const [looks, setLooks] = useState<Look[]>([]);
  const [look, setLook] = useState<Look | null>(null);
  const [lookName, setLookName] = useState("");
  const [imageId, setImage] = useState("");
  const [idles, setIdles] = useState<Idle[]>([]);
  const [imageUrl, setImageUrl] = useState("");
  const looksVersion = useRef(0);
  const idlesVersion = useRef(0);
  const currentAvatar = useRef(id);
  currentAvatar.current = id;
  const currentLook = useRef(look?.id);
  currentLook.current = look?.id;
  const loadLooks = async () => {
    if (id) {
      const version = ++looksVersion.current;
      const result = await api<Look[]>(`/avatars/${id}/looks`);
      if (version === looksVersion.current && currentAvatar.current === id)
        setLooks(result);
    }
  };
  useEffect(() => {
    setLooks([]);
    setLook(null);
    setImageUrl("");
    if (id) {
      const version = ++looksVersion.current;
      let cancelled = false;
      api<Look[]>(`/avatars/${id}/looks`)
        .then((result) => {
          if (
            !cancelled &&
            version === looksVersion.current &&
            currentAvatar.current === id
          )
            setLooks(result);
        })
        .catch((e) => {
          if (!cancelled)
            act(async () => {
              throw e;
            });
        });
      return () => {
        cancelled = true;
      };
    }
  }, [id]);
  const loadIdles = async () => {
    if (look) {
      const version = ++idlesVersion.current;
      const result = await api<Idle[]>(`/looks/${look.id}/idles`);
      if (version === idlesVersion.current && currentLook.current === look.id)
        setIdles(result);
    }
  };
  useEffect(() => {
    setIdles([]);
    if (look) {
      const version = ++idlesVersion.current;
      let cancelled = false;
      api<Idle[]>(`/looks/${look.id}/idles`)
        .then((result) => {
          if (
            !cancelled &&
            version === idlesVersion.current &&
            currentLook.current === look.id
          )
            setIdles(result);
        })
        .catch((e) => {
          if (!cancelled)
            act(async () => {
              throw e;
            });
        });
      return () => {
        cancelled = true;
      };
    }
  }, [look?.id]);
  useEffect(() => {
    setImageUrl("");
    if (!look?.imageId) return;
    let cancelled = false;
    api<import("./types").Media>(`/media/${look.imageId}`)
      .then((m) => {
        if (!cancelled) setImageUrl(m.url);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [look?.imageId]);
  const choose = (a?: Avatar) => {
    setId(a?.id || "");
    setName(a?.name || "");
    setPersona(a?.persona || "");
    setVoice(a?.voiceId || "");
    setLook(null);
    setLookName("");
    setImage("");
  };
  return (
    <fieldset disabled={busy} className="manager manager-fields">
      <section className="panel resource-list">
        <div className="section-head">
          <h2>人物</h2>
          <button onClick={() => choose()}>新建人物</button>
        </div>
        {avatars.length ? (
          avatars.map((a) => (
            <button
              className={`resource ${id === a.id ? "chosen" : ""}`}
              key={a.id}
              onClick={() => choose(a)}
            >
              <strong>{a.name}</strong>
              <small>音色 {a.voiceId}</small>
            </button>
          ))
        ) : (
          <p className="empty">还没有人物。先创建名称、人设与音色。</p>
        )}
      </section>
      <div>
        <section className="panel">
          <h2>{id ? "编辑人物" : "创建人物"}</h2>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              act(async () => {
                const a = await api<Avatar>(
                  id ? `/avatars/${id}` : "/avatars",
                  id ? "PATCH" : "POST",
                  { name, persona, voiceId },
                );
                await refresh();
                setId(a.id);
              });
            }}
          >
            <div className="two-col compact">
              <Field label="人物名称">
                <input
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </Field>
              <Field label="音色 ID">
                <input
                  required
                  value={voiceId}
                  onChange={(e) => setVoice(e.target.value)}
                  placeholder="与模型服务音色配置一致"
                />
              </Field>
            </div>
            <Field label="人物人设">
              <textarea
                rows={4}
                value={persona}
                onChange={(e) => setPersona(e.target.value)}
                placeholder="身份、表达方式与互动边界"
              />
            </Field>
            <div className="actions">
              <button
                className="primary"
                disabled={busy || !name.trim() || !voiceId.trim()}
              >
                保存人物
              </button>
              {id && (
                <button
                  type="button"
                  className="danger"
                  disabled={busy || id === lockedAvatar}
                  onClick={() => {
                    if (
                      confirm("删除此人物？关联素材与直播状态将由服务器校验。")
                    )
                      act(async () => {
                        await api(`/avatars/${id}`, "DELETE");
                        choose();
                        await refresh();
                      });
                  }}
                >
                  删除人物
                </button>
              )}
            </div>
          </form>
        </section>
        {id && (
          <section className="panel">
            <h2>形象与 idle 素材</h2>
            <p className="hint">
              上传形象图，再添加静音视频。视频应与人物外观一致，服务端会检查格式与完整解码。
            </p>
            <div className="actions wrap">
              {looks.map((l) => (
                <button
                  key={l.id}
                  className={look?.id === l.id ? "selected" : ""}
                  onClick={() => {
                    setLook(l);
                    setLookName(l.name);
                    setImage(l.imageId);
                    setImageUrl("");
                  }}
                >
                  {l.name}
                </button>
              ))}
              <button
                onClick={() => {
                  setLook(null);
                  setLookName("");
                  setImage("");
                  setImageUrl("");
                  setIdles([]);
                }}
              >
                新建形象
              </button>
            </div>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                act(async () => {
                  const l = await api<Look>(
                    look ? `/looks/${look.id}` : `/avatars/${id}/looks`,
                    look ? "PATCH" : "POST",
                    { name: lookName, imageId },
                  );
                  setLook(l);
                  await loadLooks();
                });
              }}
            >
              <Field label="形象名称">
                <input
                  required
                  value={lookName}
                  onChange={(e) => setLookName(e.target.value)}
                />
              </Field>
              <Field label="形象图（最大 30 MiB）">
                <input
                  type="file"
                  accept="image/*"
                  disabled={busy}
                  onChange={(e) => {
                    const f = e.target.files?.[0];
                    if (f)
                      act(async () => {
                        if (f.size > 30 * 1024 * 1024)
                          throw new Error("图片不能超过 30 MiB");
                        const m = await upload(f);
                        setImage(m.id);
                        setImageUrl(m.url);
                      });
                    e.target.value = "";
                  }}
                />
              </Field>
              {imageUrl && (
                <img
                  className="material-image"
                  src={imageUrl}
                  alt="已上传的形象图"
                />
              )}
              <p className="hint">
                {imageId ? "形象图已选择" : "请先上传形象图"}
              </p>
              <div className="actions">
                <button disabled={busy || !lookName.trim() || !imageId}>
                  保存形象
                </button>
                {look && (
                  <button
                    type="button"
                    className="danger"
                    disabled={busy || id === lockedAvatar}
                    onClick={() => {
                      if (confirm("删除此形象及其素材关联？"))
                        act(async () => {
                          await api(`/looks/${look.id}`, "DELETE");
                          setLook(null);
                          setImage("");
                          setLookName("");
                          await loadLooks();
                        });
                    }}
                  >
                    删除形象
                  </button>
                )}
              </div>
            </form>
            {look && (
              <div className="idle-list">
                <h3>静音 idle 视频</h3>
                {idles.length === 0 && (
                  <p className="empty">
                    还没有 idle 视频。上传后选择一条作为默认口型底片。
                  </p>
                )}
                {idles.map((i, index) => (
                  <div className="idle-row" key={i.id}>
                    <span>
                      idle {index + 1}
                      <small>{i.mediaId}</small>
                    </span>
                    <button
                      disabled={busy || look.defaultIdleId === i.id}
                      onClick={() =>
                        act(async () => {
                          const l = await api<Look>(
                            `/looks/${look.id}`,
                            "PATCH",
                            { defaultIdleId: i.id },
                          );
                          setLook(l);
                          await loadLooks();
                        })
                      }
                    >
                      {look.defaultIdleId === i.id
                        ? "默认底片"
                        : "设为默认底片"}
                    </button>
                    <button
                      className="danger"
                      disabled={busy || id === lockedAvatar}
                      onClick={() => {
                        if (confirm("删除这条 idle？"))
                          act(async () => {
                            await api(`/idles/${i.id}`, "DELETE");
                            await loadIdles();
                            await loadLooks();
                          });
                      }}
                    >
                      删除
                    </button>
                  </div>
                ))}
                <Field label="添加静音视频（最大 200 MiB）">
                  <input
                    type="file"
                    accept="video/*"
                    disabled={busy}
                    onChange={(e) => {
                      const f = e.target.files?.[0];
                      if (f)
                        act(async () => {
                          if (f.size > 200 * 1024 * 1024)
                            throw new Error("视频不能超过 200 MiB");
                          const m = await upload(f);
                          await api(`/looks/${look.id}/idles`, "POST", {
                            mediaId: m.id,
                          });
                          await loadIdles();
                        });
                      e.target.value = "";
                    }}
                  />
                </Field>
              </div>
            )}
          </section>
        )}
      </div>
    </fieldset>
  );
}
function ScriptManager({
  avatars,
  act,
  busy,
}: {
  avatars: Avatar[];
  act: Action;
  busy: boolean;
}) {
  const [avatar, setAvatar] = useState("");
  const [scripts, setScripts] = useState<Script[]>([]);
  const [kind, setKind] = useState<ScriptKind>("host");
  const [text, setText] = useState("");
  const [edit, setEdit] = useState("");
  const scriptsVersion = useRef(0);
  const currentAvatar = useRef(avatar);
  currentAvatar.current = avatar;
  const load = async () => {
    if (avatar) {
      const version = ++scriptsVersion.current;
      const result = await api<Script[]>(`/avatars/${avatar}/scripts`);
      if (
        version === scriptsVersion.current &&
        currentAvatar.current === avatar
      )
        setScripts(result);
    }
  };
  useEffect(() => {
    setScripts([]);
    setEdit("");
    setText("");
    if (avatar)
      load().catch((e) =>
        act(async () => {
          throw e;
        }),
      );
    return () => {
      scriptsVersion.current++;
    };
  }, [avatar]);
  return (
    <fieldset disabled={busy} className="two-col manager-fields">
      <section className="panel">
        <h2>编写话术</h2>
        <Field label="所属人物">
          <select value={avatar} onChange={(e) => setAvatar(e.target.value)}>
            <option value="">选择人物</option>
            {avatars.map((a) => (
              <option value={a.id} key={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </Field>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            act(async () => {
              await api(
                edit ? `/scripts/${edit}` : `/avatars/${avatar}/scripts`,
                edit ? "PATCH" : "POST",
                { kind, text },
              );
              setText("");
              setEdit("");
              await load();
            });
          }}
        >
          <Field label="话术类型">
            <select
              value={kind}
              onChange={(e) => setKind(e.target.value as ScriptKind)}
            >
              {Object.entries(kinds).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </Field>
          <Field label="话术原文">
            <textarea
              rows={8}
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder="固定稿会按原文口播，请留意时效性。"
            />
          </Field>
          <div className="actions">
            <button
              className="primary"
              disabled={busy || !avatar || !text.trim()}
            >
              {edit ? "保存修改" : "添加话术"}
            </button>
            {edit && (
              <button
                type="button"
                onClick={() => {
                  setEdit("");
                  setText("");
                }}
              >
                取消编辑
              </button>
            )}
          </div>
        </form>
      </section>
      <section className="panel">
        <h2>已保存话术</h2>
        {!avatar ? (
          <p className="empty">选择人物，查看独立话术库。</p>
        ) : scripts.length === 0 ? (
          <p className="empty">
            话术库为空，系统会保持 idle。添加主持稿后可按序循环。
          </p>
        ) : (
          Object.entries(kinds).map(([k, v]) => (
            <div className="script-group" key={k}>
              <h3>{v}</h3>
              {scripts
                .filter((s) => s.kind === k)
                .map((s) => (
                  <article className="script" key={s.id}>
                    <p>{s.text}</p>
                    <div className="actions">
                      <button
                        onClick={() => {
                          setEdit(s.id);
                          setKind(s.kind);
                          setText(s.text);
                        }}
                      >
                        编辑
                      </button>
                      <button
                        className="danger"
                        disabled={busy}
                        onClick={() =>
                          act(async () => {
                            await api(`/scripts/${s.id}`, "DELETE");
                            if (edit === s.id) {
                              setEdit("");
                              setText("");
                            }
                            await load();
                          })
                        }
                      >
                        删除
                      </button>
                    </div>
                  </article>
                ))}
            </div>
          ))
        )}
      </section>
    </fieldset>
  );
}
