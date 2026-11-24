"""Optional real FFmpeg CPU smoke; no network, models or external RTMP."""

import importlib.util
import json
import shutil
import subprocess
import tempfile
import threading
import time
import unittest
from pathlib import Path

from worker.output import LiveOutput


@unittest.skipUnless(
    importlib.util.find_spec("cv2") and shutil.which("ffmpeg"),
    "requires worker dependencies and FFmpeg",
)
class OutputSmoke(unittest.TestCase):
    def test_small_frame_mux_and_process_cleanup(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            idle = root / "idle.mp4"
            preview = root / "preview.flv"
            subprocess.run(
                [
                    "ffmpeg",
                    "-v",
                    "error",
                    "-y",
                    "-f",
                    "lavfi",
                    "-i",
                    "color=c=blue:s=128x128:r=25:d=1",
                    "-an",
                    "-c:v",
                    "libx264",
                    "-pix_fmt",
                    "yuv420p",
                    str(idle),
                ],
                check=True,
                timeout=20,
            )
            output = LiveOutput("cpu-smoke", [idle])
            output.rtmp = str(preview)
            output.start()
            cancel = threading.Event()
            errors = []

            def run():
                try:
                    output.run(cancel, lambda: False)
                except Exception as error:
                    errors.append(error)

            thread = threading.Thread(target=run)
            thread.start()
            try:
                time.sleep(2.2)
            finally:
                cancel.set()
                thread.join(10)
            self.assertFalse(thread.is_alive())
            self.assertEqual(errors, [])
            self.assertIsNone(output.proc)
            probe = json.loads(
                subprocess.check_output(
                    [
                        "ffprobe",
                        "-v",
                        "error",
                        "-show_streams",
                        "-show_format",
                        "-of",
                        "json",
                        str(preview),
                    ]
                )
            )
            self.assertEqual(
                {s["codec_type"] for s in probe["streams"]}, {"audio", "video"}
            )
            self.assertGreater(float(probe["format"]["duration"]), 1)
