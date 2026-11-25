"""Clocked BGR/PCM muxer for Linux/WSL2. Based on LingCast ffmpeg_pipe interleaving."""

import json
import os
import queue
import select
import subprocess
import threading
import time
from collections import deque
from urllib.parse import urlsplit


def terminate(process):
    if process is None:
        return
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=3)
    for stream in (process.stdin, process.stdout, process.stderr):
        if stream:
            try:
                stream.close()
            except OSError:
                pass


class LiveOutput:
    def __init__(self, sid, paths):
        self.sid = sid
        self.paths = paths
        self.proc = None
        self.relay_proc = None
        self.relay_requested = False
        self.audio = None
        self.video = None
        self.capture = None
        self.closed = threading.Event()
        self.packets = queue.Queue(maxsize=1)
        self.writer = None
        self.failure = None
        self.cancel_inference = lambda: None
        self.lifecycle = threading.RLock()
        self.children = []
        self.rtmp = os.environ.get(
            "SRS_RTMP_URL", "rtmp://srs:1935/live/{sessionId}"
        ).replace("{sessionId}", sid)

    def checked(self, args, timeout):
        with self.lifecycle:
            if self.closed.is_set():
                raise RuntimeError("output stopped")
            process = subprocess.Popen(
                args, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL
            )
            self.children.append(process)
        try:
            stdout, _ = process.communicate(timeout=timeout)
            if process.returncode:
                raise RuntimeError("invalid idle media")
            return stdout
        finally:
            with self.lifecycle:
                owned = process in self.children
                if owned:
                    self.children.remove(process)
            if owned:
                terminate(process)

    def start(self):
        import cv2

        self.cv2 = cv2
        self.width = self.height = None
        for path in self.paths:
            result = self.checked(
                ["ffprobe", "-v", "error", "-show_streams", "-of", "json", str(path)],
                30,
            )
            streams = json.loads(result)["streams"]
            videos = [s for s in streams if s["codec_type"] == "video"]
            if len(videos) != 1 or any(s["codec_type"] == "audio" for s in streams):
                raise ValueError("idle must be silent video")
            v = videos[0]
            size = (int(v["width"]), int(v["height"]))
            if min(size) < 64 or max(size) > 4096 or size[0] % 2 or size[1] % 2:
                raise ValueError("unsupported idle dimensions")
            if self.width is not None and size != (self.width, self.height):
                raise ValueError("idle dimensions must match")
            self.width, self.height = size
            rate = v["avg_frame_rate"].split("/")
            fps = float(rate[0]) / float(rate[1])
            if abs(fps - 25) > 0.01:
                raise ValueError("idle must be 25fps")
            self.checked(
                [
                    "ffmpeg",
                    "-v",
                    "error",
                    "-xerror",
                    "-i",
                    str(path),
                    "-f",
                    "null",
                    "-",
                ],
                120,
            )
        with self.lifecycle:
            if self.closed.is_set():
                raise RuntimeError("output stopped")
            ar, aw = os.pipe()
            try:
                self.proc = subprocess.Popen(
                    [
                        "ffmpeg",
                        "-v",
                        "error",
                        "-nostdin",
                        "-y",
                        "-thread_queue_size",
                        "32",
                        "-probesize",
                        "32",
                        "-analyzeduration",
                        "1",
                        "-fpsprobesize",
                        "0",
                        "-f",
                        "rawvideo",
                        "-pix_fmt",
                        "bgr24",
                        "-s",
                        f"{self.width}x{self.height}",
                        "-r",
                        "25",
                        "-i",
                        "pipe:0",
                        "-thread_queue_size",
                        "32",
                        "-probesize",
                        "32",
                        "-analyzeduration",
                        "1",
                        "-f",
                        "s16le",
                        "-ar",
                        "16000",
                        "-ac",
                        "1",
                        "-i",
                        f"pipe:{ar}",
                        "-c:v",
                        "libx264",
                        "-preset",
                        "veryfast",
                        "-tune",
                        "zerolatency",
                        "-pix_fmt",
                        "yuv420p",
                        "-g",
                        "50",
                        "-c:a",
                        "aac",
                        "-ar",
                        "44100",
                        "-f",
                        "flv",
                        "-flush_packets",
                        "1",
                        self.rtmp,
                    ],
                    stdin=subprocess.PIPE,
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                    pass_fds=(ar,),
                )
                self.audio = aw
                self.video = self.proc.stdin.fileno()
                os.set_blocking(aw, False)
                os.set_blocking(self.video, False)
            except BaseException:
                os.close(aw)
                raise
            finally:
                os.close(ar)

    def alive(self):
        process = self.proc
        return (
            not self.closed.is_set()
            and process is not None
            and process.poll() is None
            and self.failure is None
        )

    def _write(self, fd, data):
        process = self.proc
        if process is None:
            raise RuntimeError("output stopped")
        view = memoryview(data)
        deadline = time.monotonic() + 5
        while view:
            with self.lifecycle:
                if self.closed.is_set():
                    raise RuntimeError("output stopped")
                if time.monotonic() > deadline or process.poll() is not None:
                    raise RuntimeError("ffmpeg output stalled or exited")
                if select.select([], [fd], [], 0.1)[1]:
                    try:
                        view = view[os.write(fd, view) :]
                    except BlockingIOError:
                        pass

    def submit_segment(self, stream, cancel):
        # Buffer compressed pairs until this short segment is fully rendered.
        # The writer then never has to wait for an inference window mid-sentence.
        segment = deque()
        size = 0
        for pcm, frames in stream:
            if cancel.is_set() or self.closed.is_set():
                return
            if len(pcm) != len(frames) * 1280:
                raise ValueError("speech audio/video length mismatch")
            size += len(pcm) + sum(map(len, frames))
            if size > 256 * 1024 * 1024:
                raise ValueError("speech segment exceeds buffer limit")
            segment.extend((frame, pcm[i * 1280 : (i + 1) * 1280])
                           for i, frame in enumerate(frames))
        if cancel.is_set() or self.closed.is_set():
            return
        if not segment:
            raise ValueError("empty speech segment")
        self.packets.put_nowait(segment)

    def decode_frame(self, jpeg):
        import numpy as np

        image = self.cv2.imdecode(
            np.frombuffer(jpeg, dtype=np.uint8), self.cv2.IMREAD_COLOR
        )
        if image is None or image.shape != (self.height, self.width, 3):
            raise ValueError("rendered dimensions mismatch")
        return image

    def drain(self, cancel):
        while self.packets.unfinished_tasks:
            if cancel.wait(0.02) or self.closed.is_set():
                raise RuntimeError("output stopped")

    def run(self, cancel, next_task):
        def render_loop():
            try:
                while not cancel.wait(0.02):
                    next_task()
            except Exception as error:
                self.failure = error
                cancel.set()

        renderer = threading.Thread(target=render_loop, daemon=True)
        renderer.start()
        index = 0
        capture = None
        deadline = time.monotonic()
        segment = None
        try:
            while not cancel.is_set() and not self.closed.is_set():
                if segment is None:
                    try:
                        segment = self.packets.get_nowait()
                    except queue.Empty:
                        pass
                if segment is not None:
                    jpeg, audio = segment.popleft()
                    image = self.decode_frame(jpeg)
                else:
                    if capture is None:
                        capture = self.cv2.VideoCapture(str(self.paths[index]))
                    ok, image = capture.read()
                    if not ok:
                        capture.release()
                        capture = None
                        index = (index + 1) % len(self.paths)
                        continue
                    audio = bytes(1280)
                self._write(self.audio, audio)
                self._write(self.video, image.tobytes())
                deadline += 0.04
                cancel.wait(max(0, deadline - time.monotonic()))
                if segment is not None and not segment:
                    segment = None
                    self.packets.task_done()
                if time.monotonic() - deadline > 1:
                    deadline = time.monotonic()
            if self.failure:
                raise self.failure
        except Exception as error:
            self.failure = error
            raise
        finally:
            cancel.set()
            self.cancel_inference()
            if capture is not None:
                capture.release()
            self.stop()
            # Keep the session generation retiring until inference really exits.
            renderer.join()

    def relay_state(self):
        with self.lifecycle:
            if self.relay_proc is None:
                return "failed" if self.relay_requested else "disabled"
            return "running" if self.relay_proc.poll() is None else "failed"

    def relay(self, data):
        with self.lifecycle:
            terminate(self.relay_proc)
            self.relay_proc = None
            self.relay_requested = False
            if not data.get("enabled"):
                return
            server = data.get("server", "")
            key = data.get("streamKey", "")
            if (
                urlsplit(server).scheme not in ("rtmp", "rtmps")
                or not key
                or any(c in server + key for c in "\r\n")
            ):
                raise ValueError("invalid relay destination")
            if not self.alive():
                raise RuntimeError("output unavailable")
            self.relay_requested = True
            self.relay_proc = subprocess.Popen(
                [
                    "ffmpeg",
                    "-v",
                    "error",
                    "-nostdin",
                    "-i",
                    self.rtmp,
                    "-c",
                    "copy",
                    "-f",
                    "flv",
                    server.rstrip("/") + "/" + key,
                ],
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

    def stop(self):
        self.closed.set()
        with self.lifecycle:
            children, self.children = self.children, []
            self.relay_requested = False
            relay, self.relay_proc = self.relay_proc, None
            process, self.proc = self.proc, None
            audio, self.audio = self.audio, None
            self.video = None
        # Detach each resource once before slow process teardown; concurrent
        # HTTP stop and writer-finally cannot close a reused descriptor.
        for child in children:
            terminate(child)
        terminate(relay)
        terminate(process)
        if audio is not None:
            try:
                os.close(audio)
            except OSError:
                pass
        # The writer exclusively owns VideoCapture.read/release.
