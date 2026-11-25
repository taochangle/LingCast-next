import threading
import unittest

from worker.output import LiveOutput


class SpeechBufferTest(unittest.TestCase):
    def test_playback_waits_for_the_whole_short_segment(self):
        output = LiveOutput("test", [])
        cancel = threading.Event()
        first_window = threading.Event()
        finish_render = threading.Event()
        errors = []

        def render():
            yield b"a" * 1280, [b"first"]
            first_window.set()
            if not finish_render.wait(2):
                raise AssertionError("test did not release renderer")
            yield b"b" * 1280, [b"second"]

        def prepare():
            try:
                output.submit_segment(render(), cancel)
            except Exception as error:
                errors.append(error)
                first_window.set()

        thread = threading.Thread(target=prepare)
        thread.start()
        try:
            self.assertTrue(first_window.wait(1))
            self.assertEqual(errors, [])
            self.assertTrue(output.packets.empty())
        finally:
            finish_render.set()
            thread.join(2)
        self.assertFalse(thread.is_alive())
        self.assertEqual(errors, [])
        segment = output.packets.get_nowait()
        self.assertEqual(list(segment), [(b"first", b"a" * 1280), (b"second", b"b" * 1280)])
        self.assertTrue(output.packets.empty())

    def test_audio_is_prefetched_and_producer_error_reaches_the_renderer(self):
        from services.lipsync.app import MuseEngine

        engine = object.__new__(MuseEngine)
        prefetched = threading.Event()

        def read_windows(text, voice):
            yield "first"
            prefetched.set()
            yield "second"
            raise RuntimeError("TTS interrupted")

        engine._read_windows = read_windows
        windows = engine.windows("text", "voice")
        try:
            self.assertEqual(next(windows), "first")
            self.assertTrue(prefetched.wait(1))
            self.assertEqual(next(windows), "second")
            with self.assertRaisesRegex(RuntimeError, "TTS interrupted"):
                next(windows)
        finally:
            windows.close()
