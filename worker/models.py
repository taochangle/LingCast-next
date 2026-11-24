"""Authenticated model transport and strict paired PCM/JPEG protocol."""

import base64
import hashlib
import http.client
import json
import os
import socket
import threading
from pathlib import Path
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


OPENER = build_opener(NoRedirect()).open
MAX_LINE = 8 * 1024 * 1024


def download(url, path, sha=None, opener=OPENER, limit=200 * 1024 * 1024):
    if urlsplit(url).scheme not in ("http", "https"):
        raise ValueError("media requires HTTP URL")
    path = Path(path)
    digest = hashlib.sha256()
    size = 0
    try:
        with opener(Request(url), timeout=30) as response, path.open("wb") as out:
            while True:
                chunk = response.read(min(65536, limit - size + 1))
                if not chunk:
                    break
                size += len(chunk)
                if size > limit:
                    raise ValueError("media too large")
                out.write(chunk)
                digest.update(chunk)
        if not size or (sha and digest.hexdigest() != sha):
            raise ValueError("material checksum mismatch")
    except BaseException:
        path.unlink(missing_ok=True)
        raise
    return digest.hexdigest()


def bounded_lines(response):
    while True:
        line = response.readline(MAX_LINE + 1)
        if not line:
            return
        if len(line) > MAX_LINE or not line.endswith(b"\n"):
            raise ValueError("invalid NDJSON line")
        yield line


def paired_packets(lines):
    """Yield one verified window; 25fps, 16kHz s16 mono, contiguous indices."""
    pending = None
    frames = []
    index = 0
    done = False
    for line in lines:
        if len(line) > MAX_LINE:
            raise ValueError("packet too large")
        event = json.loads(line)
        if done:
            raise ValueError("data after done")
        kind = event.get("type")
        if kind == "audio":
            if pending is not None or event.get("index") != index:
                raise ValueError("audio index/order mismatch")
            count = event.get("frames")
            pcm = base64.b64decode(event.get("pcm", ""), validate=True)
            if type(count) != int or not 1 <= count <= 250 or len(pcm) != count * 1280:
                raise ValueError("PCM clock mismatch")
            pending = (pcm, count)
            frames = []
        elif kind == "frame":
            if pending is None or event.get("index") != index:
                raise ValueError("frame without matching audio")
            jpeg = base64.b64decode(event.get("jpeg", ""), validate=True)
            if not jpeg.startswith(b"\xff\xd8") or not jpeg.endswith(b"\xff\xd9"):
                raise ValueError("invalid JPEG")
            frames.append(jpeg)
            if len(frames) == pending[1]:
                yield pending[0], frames
                index += 1
                pending = None
                frames = []
        elif kind == "done":
            if pending is not None or index == 0:
                raise ValueError("incomplete rendered stream")
            done = True
        else:
            raise ValueError("renderer error or invalid packet")
    if not done or pending is not None:
        raise ValueError("incomplete rendered stream")


class ModelClient:
    def __init__(self, render_url, render_token, tts_url, tts_token):
        if not render_token or not tts_token:
            raise ValueError("model tokens required")
        self.render_url = render_url.rstrip("/")
        self.render_token = render_token
        self.tts_url = tts_url.rstrip("/")
        self.tts_token = tts_token
        self.transport_lock = threading.Lock()
        self.active_render = {}

    def request(self, url, token, data=None, timeout=60):
        body = None if data is None else json.dumps(data).encode()
        return OPENER(
            Request(
                url,
                data=body,
                headers={
                    "Authorization": "Bearer " + token,
                    "Content-Type": "application/json",
                },
            ),
            timeout=timeout,
        )

    def ready(self):
        for url, token in [
            (self.render_url, self.render_token),
            (self.tts_url, self.tts_token),
        ]:
            with self.request(url + "/readyz", token) as r:
                if not json.load(r).get("ready"):
                    return False
        return True

    def validate_voice(self, voice):
        with self.request(self.tts_url + "/voices", self.tts_token) as response:
            if voice not in json.load(response).get("profiles", {}):
                raise ValueError("unknown voice profile")

    def prepare(self, look, path, sha):
        # Binary body avoids sharing worker filesystem with remote model containers.
        with Path(path).open("rb") as body:
            request = Request(
                self.render_url + "/prepare?lookId=" + look + "&sha256=" + sha,
                data=body.read(),
                headers={
                    "Authorization": "Bearer " + self.render_token,
                    "Content-Type": "video/mp4",
                },
            )
        with OPENER(request, timeout=600) as response:
            if not json.load(response).get("ready"):
                raise RuntimeError("look preparation failed")

    def cancel_render(self):
        with self.transport_lock:
            active = list(self.active_render.items())
        for connection, transport in active:
            sock = transport or connection.sock
            if sock is not None:
                try:
                    sock.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass
            connection.close()

    def render(self, look, voice, text, sha, cancel=None):
        # Keep exact input, including whitespace; only transport/model windows split.
        remaining = text
        while remaining:
            if cancel is not None and cancel.is_set():
                return
            cut = min(240, len(remaining))
            if len(remaining) > cut:
                for position in range(cut, 119, -1):
                    if remaining[position - 1] in "。！？；.!?;\n":
                        cut = position
                        break
            chunk, remaining = remaining[:cut], remaining[cut:]
            yield from self._render_chunk(look, voice, chunk, sha, cancel)

    def _render_chunk(self, look, voice, text, sha, cancel=None):
        target = urlsplit(self.render_url + "/render")
        if target.scheme not in ("http", "https"):
            raise ValueError("model requires HTTP URL")
        connection_type = (
            http.client.HTTPSConnection
            if target.scheme == "https"
            else http.client.HTTPConnection
        )
        connection = connection_type(target.hostname, target.port, timeout=5)
        with self.transport_lock:
            if cancel is not None and cancel.is_set():
                return
            self.active_render[connection] = None
        try:
            body = json.dumps(
                dict(lookId=look, voiceId=voice, text=text, sha256=sha)
            ).encode()
            connection.request(
                "POST",
                target.path,
                body=body,
                headers={
                    "Authorization": "Bearer " + self.render_token,
                    "Content-Type": "application/json",
                },
            )
            with self.transport_lock:
                transport = connection.sock
                self.active_render[connection] = transport
            if transport is not None:
                transport.settimeout(90)
            if cancel is not None and cancel.is_set():
                return
            response = connection.getresponse()
            if response.status != 200:
                raise RuntimeError("renderer unavailable")
            # Retain the original socket even when HTTP/1.0 detaches it from
            # HTTPConnection; shutdown interrupts the response's buffered reader.
            try:
                yield from paired_packets(bounded_lines(response))
            finally:
                response.close()
        finally:
            with self.transport_lock:
                self.active_render.pop(connection, None)
            connection.close()
