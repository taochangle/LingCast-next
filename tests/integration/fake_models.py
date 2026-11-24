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
host_line = "如果把树叶放在白纸上，细细看它边缘的起伏，会发现一片叶子也有自己的轮廓。换个方向再看，也许能找到另一种有趣的形状。"
receipt_consumed = threading.Event()


def completion(messages):
    system, data = messages[0]["content"], json.loads(messages[1]["content"])
    if "只读口播审核员" in system:
        decision = {"allow": True, "category": "safe"}
    elif "topics 为12个" in system:
        decision = {"topics": [{"title": f"窗边观察{i}", "angles": ["轮廓", "色彩", "假设的场景"]} for i in range(12)], "suggestedInterests": ["日常观察"]}
    elif data["source"] == "interaction":
        decision = {"action": "speak", "text": "我喜欢浅绿色，柔和又明亮。"}
    elif any(turn["text"] == host_line and turn["source"] == "host" for turn in data["spokenTurns"]):
        receipt_consumed.set()
        decision = {"action": "skip"}
    else:
        decision = {"action": "speak", "text": host_line}
    return {"choices": [{"message": {"content": json.dumps(decision, ensure_ascii=False)}}]}

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
                return self.send_json({"texts": list(rendered), "hostReceiptConsumed": receipt_consumed.is_set()})
        return self.send_json({"error": "not found"}, 404)

    def do_POST(self):
        if not self.authorized():
            return self.send_json({"error": "unauthorized"}, 401)
        if self.server.role == "llm" and self.path == "/chat/completions":
            payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            return self.send_json(completion(payload["messages"]))
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
    llm = fixture(8093, "llm", os.environ["LLM_TOKEN"])
    threading.Thread(target=llm.serve_forever, daemon=True).start()
    tts = fixture(8091, "tts", os.environ["TTS_TOKEN"])
    threading.Thread(target=tts.serve_forever, daemon=True).start()
    fixture(8092, "renderer", os.environ["LIPSYNC_TOKEN"]).serve_forever()
