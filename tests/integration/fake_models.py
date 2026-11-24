"""CPU smoke fixture. It returns synthetic video and silence, with zero inference."""

import base64
import hashlib
import hmac
import json
import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit

import cv2
import numpy as np


VOICE = "integration-voice"
FRAMES = 25
JPEG = base64.b64encode(
    cv2.imencode(".jpg", np.full((128, 128, 3), (0, 255, 0), dtype=np.uint8))[1]
).decode()
PCM = base64.b64encode(bytes(FRAMES * 1280)).decode()
prepared = set()
rendered = []
lock = threading.Lock()


class Handler(BaseHTTPRequestHandler):
    def send(self, body, status=200, content_type="application/json"):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def send_json(self, value, status=200):
        self.send(json.dumps(value).encode(), status)

    def authorized(self):
        token = self.server.token
        return hmac.compare_digest(
            self.headers.get("Authorization", ""), "Bearer " + token
        )

    def log_message(self, *args):
        pass

    def do_GET(self):
        if not self.authorized():
            return self.send_json({"error": "unauthorized"}, 401)
        if self.path == "/readyz":
            return self.send_json({"ready": True, "fixture": "synthetic"})
        if self.server.role == "tts" and self.path == "/voices":
            return self.send_json({"profiles": {VOICE: "fixture"}})
        if self.server.role == "renderer" and self.path == "/observed":
            with lock:
                return self.send_json({"texts": list(rendered)})
        return self.send_json({"error": "not found"}, 404)

    def do_POST(self):
        if not self.authorized():
            return self.send_json({"error": "unauthorized"}, 401)
        if self.server.role != "renderer":
            return self.send_json({"error": "not found"}, 404)
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        target = urlsplit(self.path)
        if target.path == "/prepare":
            values = parse_qs(target.query)
            look = values["lookId"][0]
            sha = values["sha256"][0]
            if hashlib.sha256(body).hexdigest() != sha:
                return self.send_json({"error": "bad checksum"}, 400)
            with lock:
                prepared.add((look, sha))
            return self.send_json({"ready": True, "lookId": look, "sha256": sha})
        if target.path == "/render":
            data = json.loads(body)
            if not 1 <= len(data["text"]) <= 240:
                return self.send_json({"error": "invalid text length"}, 400)
            with lock:
                identity = data["lookId"], data["sha256"]
                if identity not in prepared or data["voiceId"] != VOICE:
                    return self.send_json({"error": "unprepared fixture"}, 400)
                rendered.append(data["text"])
            packets = [
                {"type": "audio", "index": 0, "frames": FRAMES, "pcm": PCM},
                *({"type": "frame", "index": 0, "jpeg": JPEG} for _ in range(FRAMES)),
                {"type": "done"},
            ]
            lines = b"".join(json.dumps(packet).encode() + b"\n" for packet in packets)
            return self.send(lines, content_type="application/x-ndjson")
        return self.send_json({"error": "not found"}, 404)


def fixture(port, role, token):
    instance = ThreadingHTTPServer(("0.0.0.0", port), Handler)
    instance.role = role
    instance.token = token
    return instance


if __name__ == "__main__":
    tts = fixture(8091, "tts", os.environ["TTS_TOKEN"])
    threading.Thread(target=tts.serve_forever, daemon=True).start()
    fixture(8092, "renderer", os.environ["LIPSYNC_TOKEN"]).serve_forever()
