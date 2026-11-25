import hashlib
from pathlib import Path
import json
import shutil
import subprocess
import tempfile
import unittest

from worker.media import normalize_idles

@unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'), 'requires FFmpeg')
class IdleNormalizationTest(unittest.TestCase):
    def test_24fps_becomes_25_without_changing_duration_or_original(self):
        with tempfile.TemporaryDirectory() as tmp:
            original=Path(tmp)/'original.mp4'
            subprocess.run(['ffmpeg','-v','error','-f','lavfi','-i','color=s=128x128:r=24:d=2','-an','-c:v','libx264',str(original)],check=True)
            cached=Path(tmp)/'cache.mp4';shutil.copyfile(original,cached)
            sha=hashlib.sha256(original.read_bytes()).hexdigest()
            normalize_idles([cached])
            d=json.loads(subprocess.check_output(['ffprobe','-v','error','-show_streams','-show_format','-of','json',str(cached)]))
            self.assertEqual(d['streams'][0]['avg_frame_rate'],'25/1')
            self.assertAlmostEqual(float(d['format']['duration']),2,delta=.05)
            self.assertEqual(hashlib.sha256(original.read_bytes()).hexdigest(),sha)
            after=hashlib.sha256(cached.read_bytes()).hexdigest()
            normalize_idles([cached])
            self.assertEqual(hashlib.sha256(cached.read_bytes()).hexdigest(),after)
