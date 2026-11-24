import base64
import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from worker.models import ModelClient
from worker.scheduler import Scheduler


class TextChunkTests(unittest.TestCase):
    def setUp(self):
        self.texts = []
        texts = self.texts

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                data = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                texts.append(data["text"])
                events = [
                    dict(
                        type="audio",
                        index=0,
                        frames=1,
                        pcm=base64.b64encode(bytes(1280)).decode(),
                    ),
                    dict(
                        type="frame",
                        index=0,
                        jpeg=base64.b64encode(b"\xff\xd8\xff\xd9").decode(),
                    ),
                    dict(type="done"),
                ]
                body = b"".join((json.dumps(event) + "\n").encode() for event in events)
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.client = ModelClient(
            "http://127.0.0.1:" + str(self.server.server_port),
            "renderer",
            "http://unused",
            "tts",
        )

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()

    def test_worker_queue_accepts_business_text_byte_limit(self):
        queue = Scheduler(clock=lambda: 1000)
        text = "甲" * 3000
        self.assertTrue(
            queue.add(
                dict(
                    id="long", kind="manual", text=text, createdAt=1000, expiresAt=1100
                )
            )
        )
        self.assertEqual(queue.pop()["text"], text)

    def test_long_chinese_text_preserves_every_character_in_bounded_requests(self):
        text = "甲" * 180 + "。\n" + "乙" * 240 + "，丙" * 160
        packets = list(self.client.render("look", "voice", text, "a" * 64))
        self.assertGreater(len(self.texts), 1)
        self.assertTrue(all(len(part) <= 240 for part in self.texts))
        self.assertEqual("".join(self.texts), text)
        self.assertEqual(self.texts[0], "甲" * 180 + "。\n")
        self.assertEqual(len(packets), len(self.texts))

    def test_cancellation_does_not_request_next_text_chunk(self):
        cancel = threading.Event()
        stream = self.client.render(
            "look", "voice", "甲" * 600, "a" * 64, cancel=cancel
        )
        next(stream)
        cancel.set()
        list(stream)
        self.assertEqual(len(self.texts), 1)
        self.assertLessEqual(len(self.texts[0]), 240)
