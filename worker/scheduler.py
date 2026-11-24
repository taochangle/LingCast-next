"""Per-session bounded, stable priority queue; completed IDs remain deduplicated."""

import heapq
import time
from datetime import datetime

from worker.models import split_segment

PRIORITY = {
    kind: i
    for i, kind in enumerate(("manual", "gift", "like", "comment", "welcome", "host"))
}


def timestamp(value):
    if isinstance(value, (float, int)):
        return float(value)
    return datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()


class Scheduler:
    def __init__(self, clock=time.time):
        self.clock = clock
        self.items = []
        self.seen = set()
        self.sequence = 0

    def _purge(self):
        self.items = [item for item in self.items if item[2]["_expires"] > self.clock()]
        heapq.heapify(self.items)

    def add(self, task):
        now = self.clock()
        created = timestamp(task["createdAt"])
        expires = min(timestamp(task["expiresAt"]), created + 120)
        if (
            task["kind"] not in PRIORITY
            or not isinstance(task["text"], str)
            or (not task["text"].strip() or len(task["text"].encode("utf-8")) > 10000)
        ):
            raise ValueError("invalid task")
        if created > now + 5 or not isinstance(task["id"], str) or not task["id"]:
            raise ValueError("invalid task identity/time")
        self._purge()
        if expires <= now or task["id"] in self.seen:
            return False
        if len(self.items) >= 100:
            raise OverflowError("task queue full")
        # Bound replay memory as well; refuse new work rather than forgetting old IDs.
        if len(self.seen) >= 100000:
            raise OverflowError("session task limit reached")
        self.seen.add(task["id"])
        self.sequence += 1
        heapq.heappush(
            self.items,
            (PRIORITY[task["kind"]], self.sequence, dict(task, _expires=expires, _original=task["text"])),
        )
        return True

    def pop(self):
        self._purge()
        return heapq.heappop(self.items)[2] if self.items else None

    def pop_segment(self):
        self._purge()
        if not self.items:
            return None
        priority, sequence, task = heapq.heappop(self.items)
        segment, remainder = split_segment(task["text"])
        if remainder:
            # Reserve the original queue slot and FIFO order. Expiry governs
            # admission to speech; an already-started script must finish intact.
            continuation = dict(task, text=remainder, _expires=float("inf"))
            heapq.heappush(self.items, (priority, sequence, continuation))
        return dict(task, text=segment, _final=not remainder)

    def __len__(self):
        self._purge()
        return len(self.items)
