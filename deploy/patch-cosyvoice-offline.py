"""Disable optional online text normalization in the pinned inference source.

CosyEngine always requests text_frontend=False. Tokenization and reference-audio
ONNX processing are preserved; only the unused ttsfrd/wetext initializers go away.
"""
from pathlib import Path
import sys


def disable_optional_frontend(source):
    start = '        # NOTE compatible when no text frontend tool is avaliable\n'
    end = '\n\n    def _extract_text_token('
    if source.count(start) != 1 or source.count(end) != 1:
        raise ValueError('unexpected CosyVoice frontend source; review pinned revision')
    before, rest = source.split(start, 1)
    _, after = rest.split(end, 1)
    return before + "        self.text_frontend = ''  # Offline adapter uses text_frontend=False.\n" + end + after


if __name__ == '__main__':
    target = Path(sys.argv[1])
    target.write_text(disable_optional_frontend(target.read_text()))
