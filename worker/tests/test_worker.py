import base64
import hashlib
import io
import tempfile
import unittest
from pathlib import Path

from worker.models import download, paired_packets
from worker.scheduler import Scheduler
from worker.session import SessionManager


class Models:
    def __init__(self):
        self.prepared = []

    def ready(self):
        return True

    def prepare(self, look, path, sha):
        self.prepared.append((look, sha))

    def render(self, *args):
        raise RuntimeError("renderer down")


class Output:
    def __init__(self, *a):
        self.closed = False
        self.relays = []

    def start(self):
        pass

    def stop(self):
        self.closed = True

    def alive(self):
        return not self.closed

    def run(self, *a):
        pass

    def relay_state(self):
        return "disabled"

    def relay(self, *a):
        self.relays.append(a)


class WorkerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.models = Models()
        self.outputs = []

        def output(*a):
            o = Output()
            self.outputs.append(o)
            return o

        self.manager = SessionManager(
            self.tmp.name,
            self.models,
            output,
            fetch=lambda url, path, sha=None: Path(path).write_bytes(b"idle"),
            background=False,
        )
        self.payload = dict(
            sessionId="s1",
            avatarId="any-person",
            lookId="any-look",
            voiceId="any-voice",
            imageUrl="https://business/media/image?token=x",
            idles=[
                dict(
                    id="idle",
                    url="https://business/media/idle?token=x",
                    sha256=hashlib.sha256(b"idle").hexdigest(),
                )
            ],
            defaultIdleId="idle",
            platform="douyin",
        )

    def tearDown(self):
        self.manager.close()
        self.tmp.cleanup()

    def test_stale_session(self):
        self.manager.start(self.payload)
        self.manager.stop("s1")
        self.payload["sessionId"] = "s2"
        self.manager.start(self.payload)
        for operation in [
            lambda: self.manager.stop("s1"),
            lambda: self.manager.enqueue("s1", {}),
        ]:
            with self.assertRaises(ValueError):
                operation()
        self.assertEqual(self.manager.status()["sessionId"], "s2")

    def test_long_host_yields_after_drain_then_resumes_exact_text(self):
        import time

        self.manager.start(self.payload)
        now = time.time()
        task = dict(
            id="h",
            kind="host",
            text="甲" * 240 + " 乙",
            createdAt=now,
            expiresAt=now + 60,
        )
        manual = dict(task, id="m", kind="manual", text="插播")
        spoken = []

        def render(look, voice, text, sha):
            spoken.append(text)
            if len(spoken) == 1:
                self.manager.enqueue("s1", manual)
            return []

        self.models.render = render
        drained = []
        self.outputs[0].drain = lambda cancel: drained.append(spoken[-1])
        self.manager.enqueue("s1", task)
        while self.manager.process_next():
            pass
        self.assertEqual(spoken, ["甲" * 240, "插播", " 乙"])
        self.assertEqual(drained, spoken)
        self.assertFalse(self.manager.enqueue("s1", task))

    def test_preparation_and_no_auto_relay(self):
        self.manager.start(self.payload)
        self.assertEqual(
            self.models.prepared, [("any-look", self.payload["idles"][0]["sha256"])]
        )
        self.assertEqual(self.outputs[0].relays, [])

    def test_queue_priority_expiry(self):
        q = Scheduler(clock=lambda: 1000)
        q.add(dict(id="host", kind="host", text="host", createdAt=999, expiresAt=1100))
        q.add(
            dict(
                id="manual", kind="manual", text="exact", createdAt=999, expiresAt=1100
            )
        )
        self.assertEqual(q.pop()["id"], "manual")
        self.assertEqual(q.pop()["id"], "host")
        self.assertFalse(
            q.add(dict(id="old", kind="gift", text="x", createdAt=800, expiresAt=900))
        )
        self.assertFalse(
            q.add(
                dict(
                    id="manual", kind="manual", text="x", createdAt=999, expiresAt=1100
                )
            )
        )

    def test_partial_stream_rejected(self):
        audio = dict(
            type="audio", index=0, frames=1, pcm=base64.b64encode(bytes(1280)).decode()
        )
        for events in [
            [audio],
            [audio, dict(type="done")],
            [dict(type="audio", index=2, frames=1, pcm="")],
        ]:
            with self.assertRaises(ValueError):
                list(
                    paired_packets(
                        [__import__("json").dumps(e).encode() for e in events]
                    )
                )

    def test_wrong_material_hash(self):
        self.payload["idles"][0]["sha256"] = "0" * 64
        with self.assertRaises(ValueError):
            self.manager.start(self.payload)
        self.assertEqual(self.models.prepared, [])

    def test_stop_cleans_processes(self):
        self.manager.start(self.payload)
        self.manager.stop("s1")
        self.assertTrue(self.outputs[0].closed)
        self.assertEqual(list(Path(self.tmp.name).iterdir()), [])

    def test_remote_media_auth(self):
        seen = []

        class Response(io.BytesIO):
            headers = {}

        def opener(request, timeout):
            seen.append(request.full_url)
            return Response(b"idle")

        p = Path(self.tmp.name) / "download"
        download(
            "https://business/media/id?token=secret",
            p,
            hashlib.sha256(b"idle").hexdigest(),
            opener=opener,
        )
        self.assertIn("token=secret", seen[0])
        self.assertEqual(p.read_bytes(), b"idle")

    def test_stop_during_preparation_cannot_install_late_session(self):
        import threading

        entered = threading.Event()
        release = threading.Event()
        errors = []

        def prepare(*args):
            entered.set()
            release.wait(2)

        self.models.prepare = prepare

        def start():
            try:
                self.manager.start(self.payload)
            except Exception as error:
                errors.append(error)

        thread = threading.Thread(target=start)
        thread.start()
        self.assertTrue(entered.wait(1))
        stopped = threading.Event()

        def stop():
            self.manager.stop("s1")
            stopped.set()

        stop_thread = threading.Thread(target=stop)
        stop_thread.start()
        try:
            self.assertTrue(stopped.wait(0.2), "stop blocked behind preparation")
        finally:
            release.set()
            thread.join(3)
            stop_thread.join(3)
        self.assertEqual(self.manager.status()["status"], "stopped")
        self.assertFalse(self.manager.status()["preparing"])
        self.assertEqual(self.outputs, [])

    def test_failed_start_stop_idempotent(self):
        self.payload["idles"][0]["sha256"] = "0" * 64
        with self.assertRaises(ValueError):
            self.manager.start(self.payload)
        self.manager.stop("s1")

    def test_output_death_unhealthy(self):
        self.manager.start(self.payload)
        self.outputs[0].closed = True
        self.assertEqual(self.manager.status()["status"], "error")

    def test_queue_bounds_and_expiry(self):
        now = [1000]
        q = Scheduler(clock=lambda: now[0])
        for i in range(100):
            q.add(
                dict(id=str(i), kind="host", text="x", createdAt=1000, expiresAt=5000)
            )
        with self.assertRaises(OverflowError):
            q.add(
                dict(
                    id="overflow", kind="host", text="x", createdAt=1000, expiresAt=5000
                )
            )
        now[0] = 1121
        self.assertEqual(len(q), 0)

    def test_renderer_failure(self):
        self.manager.start(self.payload)
        self.manager.enqueue(
            "s1",
            dict(
                id="t",
                kind="manual",
                text="hello",
                createdAt=__import__("time").time(),
                expiresAt=__import__("time").time() + 60,
            ),
        )
        self.manager.process_next()
        self.assertEqual(self.manager.status()["status"], "error")
        self.assertTrue(self.outputs[0].closed)


if __name__ == "__main__":
    unittest.main()
