"""Resident CosyVoice zero-shot adapter; generic immutable voice manifest."""

import base64
import os
import sys
import threading
from http.server import ThreadingHTTPServer
from pathlib import Path

from services.common import Manifest, make_handler, text_request


class CosyEngine:
    def __init__(self):
        self.ready = False
        self.failure = None
        self.busy = threading.Lock()
        self.manifest = Manifest(
            os.environ.get("MODEL_MANIFEST", "/data/manifest.json"),
            os.environ.get("MODEL_DATA_ROOT", "/data"),
        )

    def load(self):
        try:
            os.environ["HF_HUB_OFFLINE"] = "1"
            os.environ["TRANSFORMERS_OFFLINE"] = "1"
            root = Path(os.environ.get("COSYVOICE_SOURCE", "/opt/CosyVoice"))
            sys.path[:0] = [str(root), str(root / "third_party/Matcha-TTS")]
            import torch

            if not torch.cuda.is_available():
                raise RuntimeError("NVIDIA CUDA required")
            from cosyvoice.cli.cosyvoice import AutoModel

            self.model = AutoModel(
                model_dir=os.environ.get("COSYVOICE_MODEL", "/models/CosyVoice3"),
                fp16=True,
            )
            self.model.model.token_hop_len = 25
            self.model.model.stream_scale_factor = 1
            profiles = self.manifest.voices()
            if not profiles:
                raise ValueError("no configured voice references")
            self.fingerprints = self.manifest.voice_fingerprints()
            for key, value in profiles.items():
                self.model.add_zero_shot_spk(
                    "You are a helpful assistant.<|endofprompt|>" + value["promptText"],
                    str(self.manifest.resolve(value["promptAudio"])),
                    key,
                )
            self.ready = True
        except Exception as error:
            self.failure = type(error).__name__

    def events(self, data):
        text, voice = text_request(data)
        if voice not in self.fingerprints:
            raise ValueError("unknown voice")
        if self.manifest.voice_fingerprints() != self.fingerprints:
            raise ValueError("voice references changed; restart TTS")
        for part in self.model.inference_zero_shot(
            text, "", "", zero_shot_spk_id=voice, stream=True, text_frontend=False
        ):
            pcm = part["tts_speech"].float().cpu().numpy().reshape(-1).astype("<f4")
            yield dict(
                pcm=base64.b64encode(pcm.tobytes()).decode(),
                sample_rate=self.model.sample_rate,
                voiceFingerprint=self.fingerprints[voice],
            )
        yield dict(done=True, voiceFingerprint=self.fingerprints[voice])


def main():
    engine = CosyEngine()
    server = ThreadingHTTPServer(
        ("0.0.0.0", int(os.environ.get("TTS_PORT", "8091"))),
        make_handler(engine, os.environ["TTS_TOKEN"]),
    )
    threading.Thread(target=engine.load, daemon=True).start()
    server.serve_forever()


if __name__ == "__main__":
    main()
