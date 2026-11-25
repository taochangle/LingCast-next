import ast
import importlib.util
from pathlib import Path
import tarfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class OfflineFrontendTest(unittest.TestCase):
    def test_pinned_constructor_cannot_import_optional_downloaders(self):
        spec = importlib.util.spec_from_file_location('offline_patch', ROOT / 'deploy/patch-cosyvoice-offline.py')
        patch = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(patch)
        archive = next((ROOT / 'vendor/sources').glob('cosyvoice-*.tar.gz'))
        with tarfile.open(archive) as bundle:
            member = next(m for m in bundle.getmembers() if m.name.endswith('/cosyvoice/cli/frontend.py'))
            source = bundle.extractfile(member).read().decode()
        result = patch.disable_optional_frontend(source)
        constructor = next(n for n in ast.walk(ast.parse(result)) if isinstance(n, ast.FunctionDef) and n.name == '__init__')
        imports = [ast.unparse(n) for n in ast.walk(constructor) if isinstance(n, (ast.Import, ast.ImportFrom))]
        self.assertFalse(any('wetext' in n or 'ttsfrd' in n for n in imports))
        self.assertIn("self.text_frontend = ''", result)
        self.assertIn('self.speech_tokenizer_session', result)
        self.assertIn('def _extract_speech_token', result)
        with self.assertRaises(ValueError):
            patch.disable_optional_frontend('unexpected upstream revision')
