"""Shared authenticated resident model HTTP and hash-versioned preparation manifest."""

import hashlib
import hmac
import json
import re
import threading
from http.server import BaseHTTPRequestHandler
from pathlib import Path

SAFE = re.compile(r"^[A-Za-z0-9_-]{1,100}$")


class Manifest:
    def __init__(self, path, root):
        self.path = Path(path)
        self.root = Path(root).resolve()
        self.lock = threading.RLock()

    def read(self):
        return (
            json.loads(self.path.read_text())
            if self.path.exists()
            else dict(profiles={}, looks={})
        )

    def resolve(self, path):
        result = (self.root / path).resolve()
        if not result.is_relative_to(self.root):
            raise ValueError("path outside model data root")
        return result

    def voices(self):
        values = self.read().get("profiles", {})
        for key, value in values.items():
            if not SAFE.fullmatch(key) or not value.get("promptText"):
                raise ValueError("invalid voice")
            if not self.resolve(value["promptAudio"]).is_file():
                raise ValueError("missing reference audio")
        return values

    def bind(self, look, path, sha):
        if not SAFE.fullmatch(look) or not re.fullmatch("[a-f0-9]{64}", sha):
            raise ValueError("invalid material identity")
        with self.lock:
            data = self.read()
            data.setdefault("looks", {})
            self.resolve(path)
            data["looks"][look] = dict(basePath=path, sha256=sha)
            self.path.parent.mkdir(parents=True, exist_ok=True)
            pending = self.path.with_suffix(".tmp")
            pending.write_text(json.dumps(data, indent=2))
            pending.replace(self.path)

    def look(self, look, sha):
        value = self.read().get("looks", {}).get(look)
        if not value or value["sha256"] != sha:
            raise ValueError("unprepared look or wrong material")
        return value

    def voice_fingerprints(self):
        return {
            key: hashlib.sha256(
                json.dumps(value, sort_keys=True).encode()
                + self.resolve(value["promptAudio"]).read_bytes()
            ).hexdigest()
            for key, value in self.voices().items()
        }


def make_handler(engine, token):
    if not token:
        raise ValueError("model token required")

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, *args):
            pass

        def json(self, value, status=200):
            body = json.dumps(value).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def authorized(self):
            return hmac.compare_digest(
                self.headers.get("Authorization", ""), "Bearer " + token
            )

        def do_GET(self):
            if not self.authorized():
                return self.json({"error": "unauthorized"}, 401)
            if self.path == "/readyz":
                return self.json(
                    dict(ready=engine.ready, error=engine.failure),
                    200 if engine.ready else 503,
                )
            if self.path == "/voices" and hasattr(engine, "manifest"):
                return self.json({"profiles": engine.manifest.voice_fingerprints()})
            return self.json({"error": "not found"}, 404)

        def do_POST(self):
            if not self.authorized():
                self.close_connection = True
                return self.json({"error": "unauthorized"}, 401)
            if not engine.ready:
                return self.json({"error": "not ready"}, 503)
            if not engine.busy.acquire(False):
                return self.json({"error": "busy"}, 409)
            try:
                size = int(self.headers.get("Content-Length", "0"))
                prepare = self.path.startswith("/prepare?")
                if not 0 < size <= (200 * 1024 * 1024 if prepare else 8192):
                    raise ValueError("invalid request size")
                body = self.rfile.read(size)
                if len(body) != size:
                    raise ValueError("incomplete request")
                if prepare:
                    return self.json(engine.prepare(self.path, body))
                if self.path not in ("/tts", "/render"):
                    return self.json({"error": "not found"}, 404)
                data = json.loads(body)
                events = engine.events(data)
                # Validate IDs/text before headers; inference errors remain explicit NDJSON errors.
                first = next(events)
                self.send_response(200)
                self.send_header("Content-Type", "application/x-ndjson")
                self.send_header("Transfer-Encoding", "chunked")
                self.end_headers()

                def send(event):
                    payload = (json.dumps(event) + "\n").encode()
                    self.wfile.write(
                        f"{len(payload):x}\r\n".encode() + payload + b"\r\n"
                    )
                    self.wfile.flush()

                try:
                    send(first)
                    for event in events:
                        send(event)
                except Exception:
                    send({"type": "error", "error": "inference failed"})
                finally:
                    self.wfile.write(b"0\r\n\r\n")
                    self.wfile.flush()
            except (ValueError, KeyError, TypeError, StopIteration):
                self.json({"error": "invalid model request"}, 400)
            except (BrokenPipeError, ConnectionResetError):
                pass
            except Exception:
                self.json({"error": "model unavailable"}, 503)
            finally:
                engine.busy.release()

    return Handler


def text_request(data):
    text = data["text"]
    voice = data["voiceId"]
    if (
        not isinstance(text, str)
        or not 1 <= len(text.strip()) <= 240
        or not SAFE.fullmatch(voice)
    ):
        raise ValueError("invalid text/voice")
    return text, voice
