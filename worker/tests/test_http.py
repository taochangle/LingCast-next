import json
import tempfile
import threading
import unittest
from http.server import ThreadingHTTPServer
from urllib.error import HTTPError
from urllib.request import Request, urlopen

from services.common import make_handler
from worker.app import handler


class Models:
    def ready(self):
        return True


class Manager:
    models = Models()

    def status(self):
        return dict(sessionId=None, status="stopped")


class HTTPTests(unittest.TestCase):
    def serve(self, handler_type):
        server = ThreadingHTTPServer(("127.0.0.1", 0), handler_type)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        return "http://127.0.0.1:" + str(server.server_port)

    def test_worker_auth_status_and_no_secret(self):
        url = self.serve(handler(Manager(), "test-worker"))
        for path in ["/healthz", "/status", "/preview/stale.flv"]:
            with self.assertRaises(HTTPError) as caught:
                urlopen(url + path)
            self.assertEqual(caught.exception.code, 401)
            caught.exception.close()
        with urlopen(
            Request(url + "/status", headers={"Authorization": "Bearer test-worker"})
        ) as r:
            self.assertEqual(json.load(r), dict(sessionId=None, status="stopped"))

    def test_model_readiness_auth_and_failure(self):
        class Engine:
            ready = False
            failure = "RuntimeError"

        url = self.serve(make_handler(Engine(), "model-token"))
        with self.assertRaises(HTTPError) as caught:
            urlopen(url + "/readyz")
        self.assertEqual(caught.exception.code, 401)
        caught.exception.close()
        with self.assertRaises(HTTPError) as caught:
            urlopen(
                Request(
                    url + "/readyz", headers={"Authorization": "Bearer model-token"}
                )
            )
        self.assertEqual(caught.exception.code, 503)
        self.assertFalse(json.load(caught.exception)["ready"])
        caught.exception.close()
