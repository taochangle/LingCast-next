"""One live session, fixed appearance, cancellable queue and output lifecycle."""

import hashlib
import re
import shutil
import tempfile
import threading
import time
from pathlib import Path

from worker.models import download
from worker.scheduler import Scheduler

SAFE = re.compile(r"^[A-Za-z0-9_-]{1,100}$")


class SessionManager:
    def __init__(self, cache, models, output_factory, fetch=download, background=True):
        self.cache = Path(cache)
        self.cache.mkdir(parents=True, exist_ok=True)
        self.models = models
        self.output_factory = output_factory
        self.fetch = fetch
        self.background = background
        self.lock = threading.RLock()
        self.current = None
        self.output = None
        self.thread = None
        self.cancel = threading.Event()
        self.work = None
        self.last_completed = None
        self.last_stopped = None
        self.preparing = False
        self.retiring = None

    def _check(self, sid):
        if (
            not self.current
            or self.current["sessionId"] != sid
            or self.current["status"] not in ("live", "error")
        ):
            raise ValueError("stale session")

    def start(self, payload):
        with self.lock:
            if (
                (self.retiring is not None and self.retiring.is_alive())
                or self.preparing
                or (
                    self.current
                    and self.current["status"] in ("starting", "live", "error")
                )
            ):
                raise FileExistsError("session already active")
            for key in ("sessionId", "avatarId", "lookId", "voiceId"):
                if not SAFE.fullmatch(payload.get(key, "")):
                    raise ValueError("invalid identity")
            idles = payload.get("idles", [])
            if (
                not idles
                or len(idles) > 50
                or payload.get("platform") not in ("douyin", "xiaohongshu")
            ):
                raise ValueError("invalid material/platform")
            default = next(
                (i for i in idles if i["id"] == payload.get("defaultIdleId")), None
            )
            if default is None:
                raise ValueError("default idle missing")
            self.current = dict(payload, status="starting", error=None)
            self.cancel = threading.Event()
            self.queue = Scheduler()
            self.speaking = False
            self.work = Path(tempfile.mkdtemp(dir=self.cache))
            self.last_completed = None
            self.preparing = True
        try:
            if not self.models.ready():
                raise RuntimeError("models unavailable")
            if hasattr(self.models, "validate_voice"):
                self.models.validate_voice(payload["voiceId"])
            paths = []
            for i, item in enumerate(idles):
                if self.cancel.is_set():
                    raise ValueError("start cancelled")
                if not re.fullmatch("[0-9a-f]{64}", item["sha256"]):
                    raise ValueError("invalid material hash")
                path = self.work / f"{i}.mp4"
                self.fetch(item["url"], path, item["sha256"])
                if hashlib.sha256(path.read_bytes()).hexdigest() != item["sha256"]:
                    raise ValueError("material checksum mismatch")
                paths.append(path)
                if item is default:
                    self.base = path
                    self.sha = item["sha256"]
            if self.cancel.is_set():
                raise ValueError("start cancelled")
            self.models.prepare(payload["lookId"], self.base, self.sha)
            with self.lock:
                if self.cancel.is_set():
                    raise ValueError("start cancelled")
                self.output = self.output_factory(payload["sessionId"], paths)
                output = self.output
                output.cancel_inference = getattr(
                    self.models, "cancel_render", lambda: None
                )
            output.start()
            with self.lock:
                if self.cancel.is_set():
                    output.stop()
                    raise ValueError("start cancelled")
                self.current.update(
                    status="live",
                    previewUrl="/preview/" + payload["sessionId"] + ".flv",
                )
                # Publish the live generation and leave preparation atomically.
                # A stop observing live must take the full retirement/cleanup path.
                self.preparing = False
                if self.background:
                    self.thread = threading.Thread(
                        target=self._run,
                        args=(output, self.cancel, payload["sessionId"]),
                        daemon=True,
                    )
                    self.thread.start()
                return {"previewUrl": self.current["previewUrl"]}
        except BaseException:
            with self.lock:
                cancelled = self.cancel.is_set()
                self._cleanup()
                self.last_stopped = payload["sessionId"]
                if not cancelled:
                    self.current = None
                self.preparing = False
            raise

    def _run(self, output, cancel, sid):
        try:
            output.run(cancel, lambda: self.process_next(cancel))
        except Exception as error:
            with self.lock:
                # Output failures also cancel inference before joining it. The
                # stopped state, rather than cancellation alone, identifies an
                # intentional stop and preserves the original writer exception.
                if (
                    self.current
                    and self.current["sessionId"] == sid
                    and self.current["status"] not in ("stopped", "error")
                ):
                    # Inference may already have recorded the primary failure
                    # and stopped output; its teardown error is secondary.
                    self.fail(error, sid)

    def process_next(self, expected=None):
        with self.lock:
            if (
                expected is not None and expected is not self.cancel
            ) or self.cancel.is_set():
                return False
            task = self.queue.pop()
            if task is None:
                return False
            self.speaking = True
            current = self.current.copy()
            cancel = self.cancel
            output = self.output
            sha = self.sha
        try:
            args = (current["lookId"], current["voiceId"], task["text"], sha)
            stream = (
                self.models.render(*args, cancel=cancel)
                if hasattr(self.models, "cancel_render")
                else self.models.render(*args)
            )
            for pcm, frames in stream:
                if cancel.is_set():
                    return False
                output.submit(pcm, frames, cancel)
            if hasattr(output, "drain"):
                output.drain(cancel)
            with self.lock:
                if cancel is self.cancel:
                    self.last_completed = time.time()
            return True
        except Exception as error:
            if not cancel.is_set():
                self.fail(error, current["sessionId"])
            return False
        finally:
            with self.lock:
                if cancel is self.cancel:
                    self.speaking = False

    def fail(self, error, sid=None):
        with self.lock:
            if sid is not None and (
                not self.current or self.current["sessionId"] != sid
            ):
                return
            if self.current:
                self.current.update(
                    status="error", error=type(error).__name__ + ": live output failed"
                )
            self.cancel.set()
            if hasattr(self.models, "cancel_render"):
                self.models.cancel_render()
            if self.output:
                self.output.stop()

    def enqueue(self, sid, task):
        with self.lock:
            self._check(sid)
            if self.current["status"] != "live":
                raise RuntimeError("session unhealthy")
            return self.queue.add(task)

    def relay(self, sid, data):
        with self.lock:
            self._check(sid)
            self.output.relay(data)

    def _cleanup(self):
        self.cancel.set()
        if hasattr(self.models, "cancel_render"):
            self.models.cancel_render()
        if self.output:
            self.output.stop()
            self.output = None
        if self.work:
            shutil.rmtree(self.work, ignore_errors=True)
            self.work = None

    def stop(self, sid):
        with self.lock:
            if (
                self.current is None or self.current["status"] == "stopped"
            ) and sid == self.last_stopped:
                return
            if self.preparing and self.current and self.current["sessionId"] == sid:
                self.cancel.set()
                if self.output:
                    self.output.stop()
                self.current["status"] = "stopped"
                self.last_stopped = sid
                return
            self._check(sid)
            retiring = self.thread
            self.retiring = retiring
            self._cleanup()
            self.current["status"] = "stopped"
            self.last_stopped = sid
        if retiring and retiring is not threading.current_thread():
            retiring.join(timeout=5)

    def close(self):
        with self.lock:
            self._cleanup()

    def status(self):
        with self.lock:
            if not self.current:
                return dict(
                    sessionId=None,
                    status="stopped",
                    previewUrl="",
                    error=None,
                    queueDepth=0,
                    speaking=False,
                    preparing=self.preparing
                    or (self.retiring is not None and self.retiring.is_alive()),
                )
            if (
                self.current["status"] == "live"
                and self.output
                and not self.output.alive()
            ):
                self.fail(RuntimeError("ffmpeg exited"))
            return dict(
                sessionId=self.current["sessionId"],
                status=self.current["status"],
                previewUrl=self.current.get("previewUrl", ""),
                error=self.current.get("error"),
                queueDepth=len(self.queue),
                speaking=self.speaking,
                lastTaskCompletedAt=self.last_completed,
                preparing=self.preparing
                or (self.retiring is not None and self.retiring.is_alive()),
            )
