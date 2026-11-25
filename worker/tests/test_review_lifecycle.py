import tempfile
import threading
import time
import unittest
from pathlib import Path
from types import SimpleNamespace

import test_review_cleanup
from test_worker import Models, Output

from worker.output import LiveOutput
from worker.session import SessionManager


class LifecycleReviewTests(unittest.TestCase):
    payload = test_review_cleanup.ReviewCleanupTests.payload

    def test_writer_failure_cancels_renderer_and_reports_session_error(self):
        class Capture:
            def read(self):
                return True, SimpleNamespace(tobytes=lambda: b"frame")

            def release(self):
                pass

        class BrokenOutput(LiveOutput):
            def start(self):
                self.cv2 = SimpleNamespace(VideoCapture=lambda path: Capture())

            def _write(self, *args):
                raise OSError("forced writer failure")

            def alive(self):
                return not self.closed.is_set()

        models = Models()
        interrupted = threading.Event()
        models.cancel_render = interrupted.set
        with tempfile.TemporaryDirectory() as tmp:
            manager = SessionManager(
                tmp,
                models,
                BrokenOutput,
                fetch=lambda url, path, sha: Path(path).write_bytes(b"idle"),
            )
            manager.start(self.payload())
            thread = manager.thread
            thread.join(0.5)
            try:
                self.assertFalse(
                    thread.is_alive(), "writer-finally hung joining idle renderer"
                )
                self.assertTrue(interrupted.is_set())
                self.assertEqual(manager.status()["status"], "error")
                self.assertIn("OSError", manager.status()["error"])
            finally:
                manager.cancel.set()
                thread.join(2)
                manager.close()

    def test_inference_failure_is_not_overwritten_by_writer_cleanup_error(self):
        entered = threading.Event()

        class Capture:
            def read(self):
                return True, SimpleNamespace(tobytes=lambda: b"frame")

            def release(self):
                pass

        class WaitingWriter(LiveOutput):
            def start(self):
                self.cv2 = SimpleNamespace(VideoCapture=lambda path: Capture())

            def alive(self):
                return not self.closed.is_set()

            def _write(self, *args):
                entered.set()
                self.closed.wait(2)
                raise RuntimeError("output stopped during cleanup")

        class FailedInference(Models):
            def render(self, *args):
                raise ValueError("original inference failure")

        with tempfile.TemporaryDirectory() as tmp:
            manager = SessionManager(
                tmp,
                FailedInference(),
                WaitingWriter,
                fetch=lambda url, path, sha: Path(path).write_bytes(b"idle"),
            )
            manager.start(self.payload())
            try:
                self.assertTrue(entered.wait(1))
                manager.enqueue(
                    "s1",
                    dict(
                        id="fail",
                        kind="manual",
                        text="hello",
                        createdAt=time.time(),
                        expiresAt=time.time() + 30,
                    ),
                )
                manager.thread.join(2)
                self.assertFalse(manager.thread.is_alive())
                self.assertEqual(manager.status()["status"], "error")
                self.assertTrue(
                    manager.status()["error"].startswith("ValueError:"),
                    manager.status()["error"],
                )
                self.assertIsNone(manager.output.proc)
            finally:
                manager.close()

    def test_publication_stop_waits_for_retiring_generation_and_cleans_material(self):
        published = threading.Event()
        release_start = threading.Event()
        output_entered = threading.Event()
        release_output = threading.Event()
        errors = []

        class BlockedOutput(Output):
            def run(self, *args):
                output_entered.set()
                release_output.wait(4)

        with tempfile.TemporaryDirectory() as tmp:
            manager = SessionManager(
                tmp,
                Models(),
                BlockedOutput,
                fetch=lambda url, path, sha: Path(path).write_bytes(b"idle"),
            )
            original = manager.lock

            class PublicationLock:
                triggered = False

                def __enter__(self):
                    original.acquire()
                    return self

                def __exit__(self, *args):
                    should_block = (
                        threading.current_thread().name == "starting"
                        and manager.current
                        and manager.current["status"] == "live"
                        and not self.triggered
                    )
                    if should_block:
                        self.triggered = True
                    original.release()
                    if should_block:
                        published.set()
                        release_start.wait(3)

            manager.lock = PublicationLock()

            def start():
                try:
                    manager.start(self.payload())
                except Exception as error:
                    errors.append(error)

            starter = threading.Thread(target=start, name="starting")
            starter.start()
            self.assertTrue(published.wait(1))
            self.assertTrue(output_entered.wait(1))
            stopper = threading.Thread(target=lambda: manager.stop("s1"))
            stopper.start()
            try:
                deadline = time.monotonic() + 1
                while (
                    manager.status()["status"] != "stopped"
                    and time.monotonic() < deadline
                ):
                    time.sleep(0.01)
                release_start.set()
                starter.join(1)
                self.assertTrue(
                    manager.status()["preparing"],
                    "retiring generation must remain busy",
                )
                self.assertEqual(
                    list(Path(tmp).iterdir()), [], "stop bypassed material cleanup"
                )
                with self.assertRaises(FileExistsError):
                    manager.start(self.payload("s2"))
            finally:
                release_start.set()
                release_output.set()
                starter.join(3)
                stopper.join(3)
                manager.close()
            self.assertEqual(errors, [])
