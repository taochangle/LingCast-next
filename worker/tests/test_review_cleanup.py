import hashlib
import json
import os
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

from test_worker import Models, Output

from worker.models import ModelClient
from worker.output import LiveOutput
from worker.session import SessionManager


class ReviewCleanupTests(unittest.TestCase):
    def payload(self, sid="s1"):
        return dict(
            sessionId=sid,
            avatarId="avatar",
            lookId="look",
            voiceId="voice",
            imageUrl="http://business/image",
            idles=[
                dict(
                    id="idle",
                    url="http://business/idle",
                    sha256=hashlib.sha256(b"idle").hexdigest(),
                )
            ],
            defaultIdleId="idle",
            platform="douyin",
        )

    def test_stop_during_final_fetch_skips_preparation(self):
        entered = threading.Event()
        release = threading.Event()
        models = Models()
        errors = []

        def fetch(url, path, sha):
            entered.set()
            release.wait(3)
            Path(path).write_bytes(b"idle")

        with tempfile.TemporaryDirectory() as tmp:
            manager = SessionManager(tmp, models, Output, fetch=fetch, background=False)

            def start():
                try:
                    manager.start(self.payload())
                except Exception as error:
                    errors.append(error)

            thread = threading.Thread(target=start)
            thread.start()
            self.assertTrue(entered.wait(1))
            manager.stop("s1")
            release.set()
            thread.join(3)
            self.assertEqual(models.prepared, [])
            self.assertFalse(manager.status()["preparing"])
            manager.close()

    def test_concurrent_stop_does_not_release_capture_or_close_fd_twice(self):
        output = LiveOutput("s", [])

        class Capture:
            releases = 0

            def release(self):
                self.releases += 1

        capture = Capture()
        output.capture = capture
        output.audio = 123
        entered = threading.Event()
        release = threading.Event()
        closed = []

        def close(fd):
            closed.append(fd)
            entered.set()
            release.wait(2)

        with patch("worker.output.os.close", side_effect=close):
            first = threading.Thread(target=output.stop)
            first.start()
            self.assertTrue(entered.wait(1))
            second = threading.Thread(target=output.stop)
            second.start()
            second.join(0.2)
            release.set()
            first.join(2)
            second.join(2)
        self.assertEqual(closed, [123])
        self.assertEqual(
            capture.releases, 0, "stop must not release writer-owned native capture"
        )

    def test_writer_exclusively_releases_capture_after_blocked_read(self):
        from types import SimpleNamespace

        entered = threading.Event()
        release = threading.Event()
        released_by = []

        class Capture:
            def read(self):
                entered.set()
                release.wait(2)
                return False, None

            def release(self):
                released_by.append(threading.get_ident())

        capture = Capture()
        output = LiveOutput("s", ["idle"])
        output.cv2 = SimpleNamespace(VideoCapture=lambda path: capture)
        cancel = threading.Event()
        thread = threading.Thread(target=lambda: output.run(cancel, lambda: False))
        thread.start()
        self.assertTrue(entered.wait(1))
        try:
            output.stop()
            self.assertEqual(released_by, [])
        finally:
            cancel.set()
            release.set()
            thread.join(3)
        self.assertFalse(thread.is_alive())
        self.assertEqual(released_by, [thread.ident])

    def test_new_start_rejected_until_old_generation_retires(self):
        entered = threading.Event()
        release = threading.Event()
        models = Models()

        def render(*args):
            entered.set()
            release.wait(3)
            return iter(())

        models.render = render

        class RetiringOutput(Output):
            def run(self, cancel, next_task):
                while not cancel.wait(0.01):
                    next_task()

        with tempfile.TemporaryDirectory() as tmp:
            manager = SessionManager(
                tmp,
                models,
                RetiringOutput,
                fetch=lambda url, path, sha: Path(path).write_bytes(b"idle"),
            )
            manager.start(self.payload())
            manager.enqueue(
                "s1",
                dict(
                    id="t",
                    kind="manual",
                    text="hello",
                    createdAt=time.time(),
                    expiresAt=time.time() + 30,
                ),
            )
            self.assertTrue(entered.wait(1))
            thread = threading.Thread(target=lambda: manager.stop("s1"))
            thread.start()
            try:
                deadline = time.monotonic() + 1
                while (
                    manager.status()["status"] != "stopped"
                    and time.monotonic() < deadline
                ):
                    time.sleep(0.01)
                self.assertTrue(manager.status()["preparing"])
                with self.assertRaises(FileExistsError):
                    manager.start(self.payload("s2"))
            finally:
                release.set()
                thread.join(3)
            manager.start(self.payload("s2"))
            manager.close()

    def test_stop_interrupts_blocked_render_and_allows_retired_restart(self):
        entered = threading.Event()
        release = threading.Event()

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                self.rfile.read(int(self.headers["Content-Length"]))
                self.send_response(200)
                self.send_header("Content-Type", "application/x-ndjson")
                self.end_headers()
                self.wfile.flush()
                entered.set()
                release.wait(5)

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        serving = threading.Thread(target=server.serve_forever, daemon=True)
        serving.start()
        url = "http://127.0.0.1:" + str(server.server_port)
        models = ModelClient(url, "renderer", url, "tts")
        models.ready = lambda: True
        models.validate_voice = lambda voice: None
        models.prepare = lambda *args: None

        class BackgroundOutput(Output):
            def run(self, cancel, next_task):
                while not cancel.wait(0.01):
                    next_task()

        try:
            with tempfile.TemporaryDirectory() as tmp:
                manager = SessionManager(
                    tmp,
                    models,
                    BackgroundOutput,
                    fetch=lambda url, path, sha: Path(path).write_bytes(b"idle"),
                )
                manager.start(self.payload())
                manager.enqueue(
                    "s1",
                    dict(
                        id="task",
                        kind="manual",
                        text="hello",
                        createdAt=time.time(),
                        expiresAt=time.time() + 60,
                    ),
                )
                self.assertTrue(entered.wait(1))
                stop_thread = threading.Thread(target=lambda: manager.stop("s1"))
                stop_thread.start()
                stop_thread.join(1)
                try:
                    self.assertFalse(
                        stop_thread.is_alive(),
                        "stop did not interrupt renderer HTTP read",
                    )
                    manager.start(self.payload("s2"))
                    self.assertEqual(manager.status()["sessionId"], "s2")
                finally:
                    release.set()
                    stop_thread.join(6)
                    manager.close()
        finally:
            release.set()
            server.shutdown()
            server.server_close()
