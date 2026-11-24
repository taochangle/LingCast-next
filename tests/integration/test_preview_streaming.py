"""The Worker must forward a small upstream FLV packet before the stream ends."""

import os
import threading
import unittest
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from types import SimpleNamespace
from unittest.mock import patch
from urllib.request import Request, urlopen

from worker.app import handler


@contextmanager
def serve(handler_class):
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler_class)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield f"http://127.0.0.1:{server.server_port}"
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


class PreviewStreamingTest(unittest.TestCase):
    def test_tiny_flv_packet_arrives_while_upstream_is_still_open(self):
        release = threading.Event()

        class Stream(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                self.send_response(200)
                self.send_header("Content-Type", "video/x-flv")
                self.send_header("Content-Length", "6")
                self.end_headers()
                self.wfile.write(b"FLV")
                self.wfile.flush()
                if release.wait(10):
                    self.wfile.write(b"end")

        manager = SimpleNamespace(
            status=lambda: {"sessionId": "fixture", "status": "live"}
        )
        with (
            serve(Stream) as upstream,
            serve(handler(manager, "fixture-token")) as worker,
        ):
            with patch.dict(os.environ, {"SRS_HTTP_URL": upstream}):
                req = Request(
                    worker + "/preview/fixture.flv",
                    headers={"Authorization": "Bearer fixture-token"},
                )
                try:
                    with urlopen(req, timeout=2) as response:
                        self.assertEqual(response.read(3), b"FLV")
                        self.assertFalse(release.is_set())
                finally:
                    release.set()


if __name__ == "__main__":
    unittest.main()
