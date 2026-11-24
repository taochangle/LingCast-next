import hashlib
import os
import subprocess
import sys
import tempfile
import unittest
import weakref
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from services.lipsync.app import MuseEngine
from services.lipsync.runtime import StreamingMuse
from worker.output import LiveOutput


class FinalReviewTests(unittest.TestCase):
    def test_relay_child_exit_visible_until_disabled(self):
        child = subprocess.Popen(
            [sys.executable, "-c", "import sys; sys.stdin.read(1)"],
            stdin=subprocess.PIPE,
        )
        output = LiveOutput("session", [])
        try:
            with (
                patch.object(output, "alive", return_value=True),
                patch("worker.output.subprocess.Popen", return_value=child),
            ):
                output.relay(
                    dict(enabled=True, server="rtmp://localhost", streamKey="private")
                )
            self.assertEqual(output.relay_state(), "running")
            child.stdin.close()
            child.wait(timeout=5)
            self.assertEqual(output.relay_state(), "failed")
            output.relay(dict(enabled=False))
            self.assertEqual(output.relay_state(), "disabled")
        finally:
            output.stop()
            if child.poll() is None:
                child.kill()
                child.wait()

    def test_readiness_ignores_stale_look(self):
        with (
            tempfile.TemporaryDirectory() as tmp,
            patch.dict(
                os.environ,
                dict(
                    MODEL_DATA_ROOT=tmp,
                    MODEL_MANIFEST=tmp + "/manifest.json",
                    TTS_TOKEN="test",
                    TTS_URL="http://localhost",
                ),
            ),
        ):
            engine = MuseEngine()
            engine.manifest.bind("old", "missing.mp4", "a" * 64)
            with patch("services.lipsync.runtime.StreamingMuse"):
                engine.load()
            self.assertTrue(engine.ready)

    def test_cached_binding_reactivates_without_rewrite_and_render_rechecks_hash(self):
        body = b"previously decoded video"
        sha = hashlib.sha256(body).hexdigest()
        with (
            tempfile.TemporaryDirectory() as tmp,
            patch.dict(
                os.environ,
                dict(
                    MODEL_DATA_ROOT=tmp,
                    MODEL_MANIFEST=tmp + "/manifest.json",
                    TTS_TOKEN="test",
                    TTS_URL="http://localhost",
                ),
            ),
        ):
            engine = MuseEngine()
            relative = "looks/selected_" + sha + ".mp4"
            path = engine.manifest.resolve(relative)
            path.parent.mkdir()
            path.write_bytes(body)
            engine.manifest.bind("selected", relative, sha)
            with patch("services.lipsync.runtime.StreamingMuse"):
                engine.load()
            with (
                patch.object(
                    engine.manifest,
                    "bind",
                    side_effect=AssertionError("rewrote unchanged binding"),
                ),
                patch(
                    "subprocess.run",
                    side_effect=AssertionError("decoded cached source"),
                ),
            ):
                engine.prepare("/prepare?lookId=selected&sha256=" + sha, body)
            engine.model.prepare.assert_called_once_with("selected_" + sha, path, sha)
            path.write_bytes(b"modified")
            with self.assertRaisesRegex(ValueError, "modified"):
                next(
                    engine.events(
                        dict(
                            lookId="selected", sha256=sha, text="speak", voiceId="voice"
                        )
                    )
                )

    def test_old_avatar_released_before_allocating_next(self):
        class Avatar:
            input_latent_list_cycle = [1]

        previous = []
        modes = []

        def allocate(name, path, shift, batch, build):
            if previous:
                self.assertIsNone(previous[-1]())
            avatar = Avatar()
            previous.append(weakref.ref(avatar))
            modes.append(build)
            Path("results/v15/avatars", name).mkdir(parents=True, exist_ok=True)
            return avatar

        runtime = StreamingMuse.__new__(StreamingMuse)
        runtime.avatars = {}
        runtime.m = SimpleNamespace(Avatar=allocate)
        cwd = os.getcwd()
        with tempfile.TemporaryDirectory() as tmp:
            try:
                os.chdir(tmp)
                for name in ("first", "second", "first"):
                    runtime.prepare(
                        name, Path("source"), hashlib.sha256(name.encode()).hexdigest()
                    )
                self.assertEqual(modes, [True, True, False])
            finally:
                os.chdir(cwd)
