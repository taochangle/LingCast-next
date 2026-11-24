"""Exercise the real Go → Worker → local SRS path with CPU-only model fixtures."""

import json
import os
import subprocess
import sys
import time
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import Request, urlopen

app, worker, fixture = [f"http://{address}" for address in sys.argv[1:]]
admin = os.environ["ADMIN_TOKEN"]
worker_token = os.environ["WORKER_TOKEN"]
media_token = os.environ["MEDIA_TOKEN"]
model_token = os.environ["LIPSYNC_TOKEN"]


def request(
    origin, path, status, token=None, method="GET", body=None, content_type=None
):
    headers = {}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    if isinstance(body, dict):
        content_type = "application/json"
        body = json.dumps(body).encode()
    if content_type:
        headers["Content-Type"] = content_type
    try:
        response = urlopen(
            Request(origin + path, body, headers, method=method), timeout=12
        )
    except HTTPError as error:
        response = error
    with response:
        if response.status != status:
            raise AssertionError(
                f"{method} {path.split('?')[0]}: {response.status} expected {status}"
            )
        if response.headers.get_content_type() == "application/json":
            return json.load(response)
        if response.headers.get_content_type() == "video/x-flv":
            return response.read(3)
        return response.read()


def eventually(check, timeout=25):
    end = time.monotonic() + timeout
    while True:
        try:
            if check():
                return
        except (AssertionError, ConnectionError, OSError):
            if time.monotonic() >= end:
                raise
        if time.monotonic() >= end:
            raise AssertionError("Timed out waiting for integration condition")
        time.sleep(0.25)


def wait_for_speech():
    def idle_queue():
        status = request(worker, "/status", 200, worker_token)
        assert status["status"] == "live", f"Worker status {status['status']}"
        return not status["speaking"] and status["queueDepth"] == 0

    eventually(idle_queue)


def ffmpeg(*args):
    return subprocess.check_output(
        ["ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", *args],
        timeout=15,
    )


def upload(name, data, content_type):
    delimiter = "--lingcast-integration-fixture"
    body = (
        (
            f'{delimiter}\r\nContent-Disposition: form-data; name="file"; filename="{name}"'
            f"\r\nContent-Type: {content_type}\r\n\r\n"
        ).encode()
        + data
        + f"\r\n{delimiter}--\r\n".encode()
    )
    return request(
        app,
        "/api/media",
        201,
        admin,
        "POST",
        body,
        f"multipart/form-data; boundary={delimiter[2:]}",
    )


def verify():
    eventually(lambda: request(app, "/healthz", 200))
    eventually(lambda: request(worker, "/healthz", 200, worker_token)["ready"])
    assert b'id="root"' in request(app, "/", 200)
    request(app, "/api/avatars", 401)
    request(worker, "/status", 401)
    print("Go/UI and Worker ready; unauthenticated control denied", flush=True)

    image = ffmpeg(
        "-f",
        "lavfi",
        "-i",
        "color=c=blue:s=128x128",
        "-frames:v",
        "1",
        "-threads",
        "1",
        "-c:v",
        "png",
        "-f",
        "image2pipe",
        "pipe:1",
    )
    idle = ffmpeg(
        "-f",
        "lavfi",
        "-i",
        "color=c=blue:s=128x128:r=25",
        "-t",
        "2",
        "-an",
        "-threads",
        "1",
        "-c:v",
        "libx264",
        "-pix_fmt",
        "yuv420p",
        "-movflags",
        "frag_keyframe+empty_moov",
        "-f",
        "mp4",
        "pipe:1",
    )
    image_row = upload("image.png", image, "image/png")
    idle_row = upload("idle.mp4", idle, "video/mp4")
    assert image_row["url"].startswith("http://app:8080/media/")
    request(app, f"/media/{image_row['id']}", 401)
    query = urlencode({"token": media_token})
    assert request(app, f"/media/{image_row['id']}?{query}", 200) == image

    avatar = request(
        app,
        "/api/avatars",
        201,
        admin,
        "POST",
        {
            "name": "Integration Avatar",
            "persona": "CPU test fixture",
            "voiceId": "integration-voice",
        },
    )
    avatar_path = f"/api/avatars/{avatar['id']}"
    look = request(
        app,
        f"{avatar_path}/looks",
        201,
        admin,
        "POST",
        {
            "name": "Integration Look",
            "imageId": image_row["id"],
        },
    )
    look_path = f"/api/looks/{look['id']}"
    idle_ref = request(
        app, f"{look_path}/idles", 201, admin, "POST", {"mediaId": idle_row["id"]}
    )
    request(app, look_path, 200, admin, "PATCH", {"defaultIdleId": idle_ref["id"]})
    fixed = "欢迎来到合成测试。"
    request(
        app,
        f"{avatar_path}/scripts",
        201,
        admin,
        "POST",
        {"kind": "welcome", "text": fixed},
    )

    start = {"avatarId": avatar["id"], "lookId": look["id"], "platform": "douyin"}
    live = request(app, "/api/live", 201, admin, "POST", start)
    assert live["session"]["status"] == "live"
    sid = live["session"]["id"]
    live_path = f"/api/live/{sid}"
    collector = live["collectorToken"]
    request(app, "/api/live", 409, admin, "POST", start)
    request(worker, f"/preview/{sid}.flv", 401)
    preview = live["session"]["previewUrl"]
    request(app, preview, 401)
    browser_preview = f"{preview}?{urlencode({'token': admin})}"
    eventually(lambda: request(app, browser_preview, 200) == b"FLV")
    print(
        "Signed media, real Worker startup and private SRS FLV preview verified",
        flush=True,
    )

    manual = "手动口播，原文。"
    assert request(app, f"{live_path}/speak", 200, admin, "POST", {"text": manual})[
        "queued"
    ]
    event = {
        "sessionId": sid,
        "platform": "douyin",
        "events": [
            {
                "id": "fixture-enter-1",
                "kind": "enter",
                "nickname": "Synthetic viewer",
                "text": "",
            }
        ],
    }
    request(
        app,
        "/api/collector/events",
        400,
        collector,
        "POST",
        dict(event, platform="xiaohongshu"),
    )
    event_id = event["events"][0]["id"]
    for expected in (1, 0):
        ack = request(app, "/api/collector/events", 200, collector, "POST", event)
        assert ack["accepted"] == expected
        assert ack["processedIds"] == [event_id]
        assert ack["retryIds"] == []
    eventually(
        lambda: (
            request(fixture, "/observed", 200, model_token)["texts"] == [manual, fixed]
        )
    )
    wait_for_speech()

    long_text = "这是需要分段的合成口播。" * 24
    assert request(app, f"{live_path}/speak", 200, admin, "POST", {"text": long_text})[
        "queued"
    ]
    eventually(
        lambda: (
            "".join(request(fixture, "/observed", 200, model_token)["texts"][2:])
            == long_text
        )
    )
    wait_for_speech()
    print(
        "Manual speech, deduplicated events and long text reached the synthetic renderer verbatim",
        flush=True,
    )

    assert request(app, f"{live_path}/stop", 200, admin, "POST")["status"] == "stopped"
    assert request(app, "/api/live", 200, admin)["session"] is None
    assert request(worker, "/status", 200, worker_token)["status"] == "stopped"
    request(app, "/api/collector/events", 401, collector, "POST", event)
    request(app, f"{live_path}/speak", 409, admin, "POST", {"text": manual})
    request(worker, f"/sessions/{sid}/tasks", 400, worker_token, "POST", {})
    print(
        "Stop invalidated the session, its collector token and stale tasks", flush=True
    )


if __name__ == "__main__":
    verify()
