export interface Avatar {
  id: string;
  name: string;
  persona: string;
  voiceId: string;
}
export interface Look {
  id: string;
  avatarId: string;
  name: string;
  imageId: string;
  defaultIdleId: string;
}
export interface Idle {
  id: string;
  lookId: string;
  mediaId: string;
}
export type ScriptKind = "host" | "welcome" | "gift" | "like" | "comment";
export interface Script {
  id: string;
  avatarId: string;
  kind: ScriptKind;
  text: string;
}
export type Platform = "douyin" | "xiaohongshu";
export interface Session {
  id: string;
  avatarId: string;
  lookId: string;
  platform: Platform;
  status: string;
  hostEnabled: boolean;
  previewUrl: string;
  error: string;
  createdAt: string;
}
export interface Media {
  id: string;
  url: string;
  kind: string;
  width: number;
  height: number;
  fps: number;
  sha256: string;
}
export interface Settings {
  timezone: string;
  workerUrl: string;
  llmUrl: string;
  llmModel: string;
  configured: Record<string, boolean>;
}
