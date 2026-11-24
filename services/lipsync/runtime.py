"""Adapted LingCast StreamingMuse: no built-in avatars, explicit generic prepare."""

import copy
import os
import sys
import threading
import types
from pathlib import Path


class StreamingMuse:
    def __init__(self):
        os.environ["HF_HUB_OFFLINE"] = "1"
        os.environ["TRANSFORMERS_OFFLINE"] = "1"
        source = Path(os.environ.get("MUSETALK_SOURCE", "/opt/MuseTalk")).resolve()
        sys.path[:0] = [str(source), str(source / "musetalk/utils")]
        # MuseTalk resolves model and result paths relative to cwd. Model assets are
        # mounted at cache/models; writable results remain in persistent cache.
        cache = Path(os.environ.get("MUSETALK_CACHE", "/cache/musetalk")).resolve()
        cache.mkdir(parents=True, exist_ok=True)
        # DWPose's pinned config path is relative to cwd, unlike its Python imports.
        package = cache / "musetalk"
        if not package.exists():
            package.symlink_to(source / "musetalk", target_is_directory=True)
        os.chdir(cache)
        import cv2
        import torch

        if not torch.cuda.is_available():
            raise RuntimeError("NVIDIA CUDA required")
        # Pinned upstream would otherwise download S3FD silently at startup.
        detector_path = cache.resolve() / "models/s3fd/s3fd.pth"
        if not detector_path.is_file():
            raise FileNotFoundError("operator must mount models/s3fd/s3fd.pth")
        from face_detection.detection.sfd.sfd_detector import SFDDetector

        original_init = SFDDetector.__init__

        def detector_init(instance, device, **kwargs):
            return original_init(
                instance, device, path_to_detector=str(detector_path), **kwargs
            )

        SFDDetector.__init__ = detector_init
        import scripts.realtime_inference as m

        self.m = m
        self.cv2 = cv2
        self.avatars = {}
        m.args = types.SimpleNamespace(
            version="v15",
            extra_margin=10,
            parsing_mode="jaw",
            skip_save_images=True,
            audio_padding_length_left=2,
            audio_padding_length_right=2,
        )
        m.device = torch.device("cuda:0")
        m.vae, m.unet, m.pe = m.load_all_model(
            unet_model_path="models/musetalkV15/unet.pth",
            vae_type="sd-vae",
            unet_config="models/musetalkV15/musetalk.json",
            device=m.device,
        )
        m.timesteps = torch.tensor([0], device=m.device)
        m.pe = m.pe.half().to(m.device)
        m.vae.vae = m.vae.vae.half().to(m.device)
        m.unet.model = m.unet.model.half().to(m.device)
        m.audio_processor = m.AudioProcessor(feature_extractor_path="models/whisper")
        m.weight_dtype = m.unet.model.dtype
        m.whisper = (
            m.WhisperModel.from_pretrained("models/whisper")
            .to(device=m.device, dtype=m.weight_dtype)
            .eval()
        )
        m.whisper.requires_grad_(False)
        m.fp = m.FaceParsing(left_cheek_width=90, right_cheek_width=90)

    def prepare(self, name, path, sha):
        m = self.m
        profile = Path("results/v15/avatars") / name
        marker = profile / "source.sha256"
        prepared = marker.is_file() and marker.read_text().strip() == sha
        if profile.exists() and not prepared:
            import shutil

            shutil.rmtree(profile)
        if prepared and name in self.avatars:
            return
        self.avatars.clear()
        avatar = m.Avatar(name, str(path), 0, 8, not prepared)
        if not avatar.input_latent_list_cycle:
            raise ValueError("no face prepared")
        if not prepared:
            marker.write_text(sha)
        self.avatars[name] = avatar

    def render(self, name, audio, callback, frame_offset=0):
        m = self.m
        cv2 = self.cv2
        avatar = copy.copy(self.avatars[name])
        errors = []
        for key in [
            "frame_list_cycle",
            "coord_list_cycle",
            "input_latent_list_cycle",
            "mask_list_cycle",
            "mask_coords_list_cycle",
        ]:
            values = getattr(avatar, key)
            offset = frame_offset % len(values)
            setattr(avatar, key, values[offset:] + values[:offset])

        def consume(instance, q, n, skip):
            try:
                for idx in range(n):
                    face = q.get(timeout=60)
                    bbox = instance.coord_list_cycle[
                        idx % len(instance.coord_list_cycle)
                    ]
                    frame = copy.deepcopy(
                        instance.frame_list_cycle[idx % len(instance.frame_list_cycle)]
                    )
                    x1, y1, x2, y2 = bbox
                    face = cv2.resize(face.astype("uint8"), (x2 - x1, y2 - y1))
                    frame = m.get_image_blending(
                        frame,
                        face,
                        bbox,
                        instance.mask_list_cycle[idx % len(instance.mask_list_cycle)],
                        instance.mask_coords_list_cycle[
                            idx % len(instance.mask_coords_list_cycle)
                        ],
                    )
                    callback(frame, idx)
                    instance.idx = idx + 1
            except Exception as error:
                errors.append(error)

        avatar.process_frames = types.MethodType(consume, avatar)
        try:
            avatar.inference(str(audio), None, 25, True)
        finally:
            # The bound callback otherwise keeps this copy and its frame lists alive.
            del avatar.process_frames
        if errors:
            raise RuntimeError("MuseTalk frame consumer failed") from errors[0]
