"""Normalize downloaded private idle copies to the 25fps output clock."""
import json
from pathlib import Path
import subprocess


def normalize_idles(paths):
    for value in paths:
        path = Path(value)
        streams = json.loads(subprocess.check_output(
            ['ffprobe', '-v', 'error', '-show_streams', '-of', 'json', str(path)], timeout=30
        ))['streams']
        videos = [s for s in streams if s['codec_type'] == 'video']
        if len(videos) != 1 or any(s['codec_type'] == 'audio' for s in streams):
            raise ValueError('idle must be silent video')
        num, den = map(float, videos[0]['avg_frame_rate'].split('/'))
        if den <= 0 or not 0 < num / den <= 120:
            raise ValueError('unsupported idle frame rate')
        if abs(num / den - 25) <= .01:
            continue
        target = path.with_suffix('.normalized.mp4')
        try:
            subprocess.run([
                'ffmpeg', '-v', 'error', '-nostdin', '-y', '-threads', '2', '-i', str(path),
                '-map', '0:v:0', '-vf', 'fps=25', '-an', '-c:v', 'libx264',
                '-threads', '2', '-preset', 'fast', '-crf', '18', '-pix_fmt', 'yuv420p', str(target)
            ], check=True, timeout=120, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if target.stat().st_size > 200 * 1024 * 1024:
                raise ValueError('normalized idle exceeds size limit')
            target.replace(path)
        finally:
            target.unlink(missing_ok=True)
