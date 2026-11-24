import json
import tempfile
import unittest
from pathlib import Path

from services.common import Manifest


class ServiceTests(unittest.TestCase):
    def test_generic_voice_and_path_boundary(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "voice.wav").write_bytes(b"wav")
            (root / "manifest.json").write_text(
                json.dumps(
                    dict(
                        profiles={
                            "arbitrary-voice": dict(
                                promptText="你好", promptAudio="voice.wav"
                            )
                        },
                        looks={},
                    )
                )
            )
            manifest = Manifest(root / "manifest.json", root)
            self.assertEqual(manifest.voices()["arbitrary-voice"]["promptText"], "你好")
            (root / "manifest.json").write_text(
                json.dumps(
                    dict(
                        profiles={"bad": dict(promptText="x", promptAudio="../secret")},
                        looks={},
                    )
                )
            )
            with self.assertRaises(ValueError):
                manifest.voices()

    def test_material_registration_hash_versioned(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            m = Manifest(root / "manifest.json", root)
            m.bind("look", "base.mp4", "a" * 64)
            self.assertEqual(m.look("look", "a" * 64)["basePath"], "base.mp4")
            m.bind("look", "new.mp4", "b" * 64)
            self.assertEqual(m.look("look", "b" * 64)["basePath"], "new.mp4")
            with self.assertRaises(ValueError):
                m.look("look", "a" * 64)
