"""Worker REST. All endpoints including preview and readiness require Bearer auth."""

import hmac
import json
import os
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.request import Request

from worker.models import OPENER, ModelClient
from worker.output import LiveOutput
from worker.session import SessionManager


def handler(manager, token):
    if not token:
        raise ValueError("WORKER_TOKEN required")

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def json(self, data, status=200):
            payload = json.dumps(data).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

        def do_GET(self):
            self.dispatch("GET")

        def do_POST(self):
            self.dispatch("POST")

        def do_PUT(self):
            self.dispatch("PUT")

        def dispatch(self, method):
            if not hmac.compare_digest(
                self.headers.get("Authorization", ""), "Bearer " + token
            ):
                return self.json({"error": "unauthorized"}, 401)
            try:
                if method == "GET":
                    if self.path == "/healthz":
                        try:
                            ready = (
                                manager.models.ready()
                                and manager.status()["status"] != "error"
                            )
                        except Exception:
                            ready = False
                        return self.json({"ready": ready}, 200 if ready else 503)
                    match = re.fullmatch(r"/sessions/([A-Za-z0-9_-]+)/task-results(?:\?after=([0-9]+))?", self.path)
                    if match:
                        return self.json(manager.task_results(match[1], int(match[2] or 0)))
                    if self.path == "/status":
                        return self.json(manager.status())
                    match = re.fullmatch(r"/preview/([A-Za-z0-9_-]+)\.flv", self.path)
                    if match:
                        state = manager.status()
                        if state["sessionId"] != match[1] or state["status"] != "live":
                            raise ValueError("stale session")
                        url = (
                            os.environ.get("SRS_HTTP_URL", "http://srs:8080").rstrip(
                                "/"
                            )
                            + "/live/"
                            + match[1]
                            + ".flv"
                        )
                        with OPENER(Request(url), timeout=15) as response:
                            self.send_response(200)
                            self.send_header("Content-Type", "video/x-flv")
                            self.send_header("Cache-Control", "no-store")
                            self.end_headers()
                            while chunk := response.read1(65536):
                                self.wfile.write(chunk)
                                self.wfile.flush()
                        return
                size = int(self.headers.get("Content-Length", "0"))
                if not 0 <= size <= 65536:
                    raise ValueError("request too large")
                data = json.loads(self.rfile.read(size)) if size else {}
                if method == "POST" and self.path == "/sessions":
                    return self.json(manager.start(data), 201)
                match = re.fullmatch(
                    r"/sessions/([A-Za-z0-9_-]+)/(stop|tasks|relay)", self.path
                )
                if match:
                    sid, action = match.groups()
                    if method == "POST" and action == "stop":
                        manager.stop(sid)
                        return self.json({"ok": True})
                    if method == "POST" and action == "tasks":
                        return self.json({"accepted": manager.enqueue(sid, data)}, 201)
                    if method == "PUT" and action == "relay":
                        manager.relay(sid, data)
                        return self.json({"ok": True})
                return self.json({"error": "not found"}, 404)
            except FileExistsError:
                return self.json({"error": "session already active"}, 409)
            except OverflowError:
                return self.json({"error": "queue full"}, 409)
            except (ValueError, KeyError, TypeError):
                return self.json({"error": "invalid request or stale session"}, 400)
            except (BrokenPipeError, ConnectionResetError):
                return
            except Exception:
                return self.json({"error": "worker unavailable"}, 503)

    return Handler


def main():
    models = ModelClient(
        os.environ["LIPSYNC_URL"],
        os.environ["LIPSYNC_TOKEN"],
        os.environ["TTS_URL"],
        os.environ["TTS_TOKEN"],
    )
    manager = SessionManager(
        os.environ.get("WORKER_CACHE_DIR", "/cache/worker"), models, LiveOutput
    )
    server = ThreadingHTTPServer(
        ("0.0.0.0", int(os.environ.get("WORKER_PORT", "8090"))),
        handler(manager, os.environ["WORKER_TOKEN"]),
    )
    try:
        server.serve_forever()
    finally:
        manager.close()
        server.server_close()


if __name__ == "__main__":
    main()
