import time
import unittest
from datetime import datetime

from worker.tests import test_worker


class TaskResults(unittest.TestCase):
    setUp = test_worker.WorkerTests.setUp
    tearDown = test_worker.WorkerTests.tearDown

    def begin(self):
        self.manager.start(self.payload)
        self.models.render = lambda *args: [(b"pcm", b"frames")]
        self.outputs[-1].submit = lambda *args: None

    def enqueue(self, task_id, text="hello"):
        now = time.time()
        self.manager.enqueue("s1", dict(id=task_id, kind="host", text=text,
                                        createdAt=now, expiresAt=now+100))

    def test_result_only_after_final_segment(self):
        self.begin()
        text = "甲" * 240 + " 乙"
        self.enqueue("h", text)
        self.manager.process_next()
        self.assertEqual(self.manager.task_results("s1", 0)["results"], [])
        self.manager.process_next()
        result, = self.manager.task_results("s1", 0)["results"]
        self.assertEqual((result["state"], result["text"]), ("completed", text))
        self.assertIsNotNone(datetime.fromisoformat(result["completedAt"]).tzinfo)

    def test_failed_or_cancelled_not_completed(self):
        self.begin()
        self.enqueue("h")
        self.outputs[0].drain = lambda cancel: cancel.set()
        self.manager.process_next()
        self.assertFalse(self.manager.task_results("s1", 0)["results"])
        self.manager.stop("s1")
        self.begin()
        self.enqueue("f")
        self.models.render = test_worker.Models().render
        self.manager.process_next()
        result, = self.manager.task_results("s1", 0)["results"]
        self.assertEqual((result["state"], result["text"]), ("failed", ""))

    def test_results_bounded_and_session_scoped(self):
        self.begin()
        for i in range(257):
            self.enqueue(str(i))
            self.manager.process_next()
        page = self.manager.task_results("s1", 0)
        self.assertEqual(len(page["results"]), 256)
        self.assertEqual(page["nextSeq"], 257)
        self.assertTrue(page["truncated"])
        self.assertFalse(self.manager.task_results("s1", 1)["truncated"])
        self.manager.stop("s1")
        with self.assertRaises(ValueError):
            self.manager.task_results("s1", 0)
        self.payload["sessionId"] = "s2"
        self.begin()
        self.assertEqual(self.manager.task_results("s2", 0)["nextSeq"], 0)
