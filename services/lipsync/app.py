"""Authenticated CosyVoice -> MuseTalk paired PCM/JPEG stream and preparation."""

import base64
import hashlib
import json
import math
import os
import re
import tempfile
import threading
from http.server import ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit
from urllib.request import Request

from services.common import SAFE, Manifest, make_handler, text_request
from worker.models import OPENER, bounded_lines


def file_sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


class MuseEngine:
    def __init__(self):
        self.ready = False
        self.failure = None
        self.busy = threading.Lock()
        self.manifest = Manifest(
            os.environ.get("MODEL_MANIFEST", "/data/manifest.json"),
            os.environ.get("MODEL_DATA_ROOT", "/data"),
        )
        self.tts = os.environ["TTS_URL"].rstrip("/")
        self.token = os.environ["TTS_TOKEN"]
        if not self.token:
            raise ValueError("TTS_TOKEN required")

    def load(self):
        try:
            from services.lipsync.runtime import StreamingMuse

            self.model = StreamingMuse()
            self.ready = True
        except Exception as error:
            self.failure = type(error).__name__

    def prepare(self, url, body):
        params = parse_qs(urlsplit(url).query)
        look = params["lookId"][0]
        sha = params["sha256"][0]
        if (
            not SAFE.fullmatch(look)
            or not re.fullmatch("[a-f0-9]{64}", sha)
            or hashlib.sha256(body).hexdigest() != sha
        ):
            raise ValueError("invalid material checksum/identity")
        old = self.manifest.read().get("looks", {}).get(look)
        path = self.manifest.resolve("looks/" + look + "_" + sha + ".mp4")
        relative = str(path.relative_to(self.manifest.root))
        if path.is_file() and file_sha256(path) == sha:
            self.model.prepare(look + "_" + sha, path, sha)
            if old != dict(basePath=relative, sha256=sha):
                self.manifest.bind(look, relative, sha)
            return dict(ready=True, lookId=look, sha256=sha)
        path.parent.mkdir(parents=True, exist_ok=True)
        pending = path.with_suffix(".tmp")
        pending.write_bytes(body)
        try:
            import subprocess

            subprocess.run(
                [
                    "ffmpeg",
                    "-v",
                    "error",
                    "-xerror",
                    "-i",
                    str(pending),
                    "-f",
                    "null",
                    "-",
                ],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                timeout=120,
                check=True,
            )
            pending.replace(path)
            self.model.prepare(look + "_" + sha, path, sha)
            self.manifest.bind(look, str(path.relative_to(self.manifest.root)), sha)
        except BaseException:
            pending.unlink(missing_ok=True)
            if not old:
                path.unlink(missing_ok=True)
            raise
        return dict(ready=True, lookId=look, sha256=sha)

    def windows(self, text, voice):
        import numpy as np

        from services.lipsync.audio_windows import AudioWindows

        request = Request(
            self.tts + "/tts",
            data=json.dumps(dict(text=text, voiceId=voice)).encode(),
            headers={
                "Authorization": "Bearer " + self.token,
                "Content-Type": "application/json",
            },
        )
        assembler = None
        done = False
        fingerprint = None
        with OPENER(request, timeout=90) as response:
            for line in bounded_lines(response):
                part = json.loads(line)
                if part.get("error") or part.get("type") == "error":
                    raise RuntimeError("TTS failed")
                if fingerprint is None:
                    fingerprint = part["voiceFingerprint"]
                if part.get("voiceFingerprint") != fingerprint:
                    raise ValueError("voice changed mid-stream")
                if part.get("done"):
                    done = True
                    if assembler:
                        for core, window, left in assembler.feed(
                            np.empty(0, dtype=np.float32), final=True
                        ):
                            yield core, window, left, assembler.rate, fingerprint
                    break
                rate = int(part["sample_rate"])
                assembler = assembler or AudioWindows(rate)
                raw = base64.b64decode(part["pcm"], validate=True)
                if rate != assembler.rate or len(raw) % 4:
                    raise ValueError("invalid PCM rate/format")
                pcm = np.frombuffer(raw, dtype="<f4")
                if not np.isfinite(pcm).all():
                    raise ValueError("non-finite PCM")
                for core, window, left in assembler.feed(pcm):
                    yield core, window, left, rate, fingerprint
        if not done:
            raise ValueError("incomplete CosyVoice stream")

    def events(self, data):
        text, voice = text_request(data)
        look = data["lookId"]
        sha = data["sha256"]
        binding = self.manifest.look(look, sha)
        if file_sha256(self.manifest.resolve(binding["basePath"])) != sha:
            raise ValueError("prepared material modified")
        profile = look + "_" + sha
        if profile not in self.model.avatars:
            raise ValueError("look not prepared")
        import cv2
        import numpy as np
        import soundfile as sf
        from scipy.signal import resample_poly

        offset = 0
        with tempfile.TemporaryDirectory(
            dir=os.environ.get("MUSETALK_CACHE", "/cache/musetalk")
        ) as work:
            for index, (core, window, left, rate, fingerprint) in enumerate(
                self.windows(text, voice)
            ):
                count = math.ceil(len(core) / rate * 25)
                path = Path(work) / "window.wav"
                sf.write(path, window, rate, subtype="PCM_16")
                divisor = math.gcd(rate, 16000)
                pcm = resample_poly(core, 16000 // divisor, rate // divisor)
                pcm = np.pad(pcm, (0, max(0, count * 640 - len(pcm))))[: count * 640]
                pcm = (np.clip(pcm, -1, 1) * 32767).astype("<i2").tobytes()
                digest = hashlib.sha256(
                    json.dumps(
                        [
                            "musetalk-0a89dec4",
                            "cosyvoice-074ca6dc",
                            sha,
                            voice,
                            fingerprint,
                            text,
                        ],
                        ensure_ascii=False,
                    ).encode()
                ).hexdigest()
                yield dict(
                    type="audio",
                    index=index,
                    frames=count,
                    pcm=base64.b64encode(pcm).decode(),
                    fingerprint=digest,
                )
                # One bounded one-second window; inference's callback thread writes here.
                images = []

                def frame(image, number):
                    if left <= number < left + count:
                        ok, jpeg = cv2.imencode(
                            ".jpg", image, [cv2.IMWRITE_JPEG_QUALITY, 85]
                        )
                        if not ok:
                            raise RuntimeError("JPEG encoding failed")
                        images.append(base64.b64encode(jpeg).decode())

                self.model.render(profile, path, frame, frame_offset=offset - left)
                if len(images) != count:
                    raise RuntimeError("incomplete MuseTalk window")
                for jpeg in images:
                    yield dict(type="frame", index=index, jpeg=jpeg)
                offset += count
        yield dict(type="done")


def main():
    engine = MuseEngine()
    server = ThreadingHTTPServer(
        ("0.0.0.0", int(os.environ.get("LIPSYNC_PORT", "8092"))),
        make_handler(engine, os.environ["LIPSYNC_TOKEN"]),
    )
    threading.Thread(target=engine.load, daemon=True).start()
    server.serve_forever()


if __name__ == "__main__":
    main()
