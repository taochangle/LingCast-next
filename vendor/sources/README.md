# Pinned inference source bundles

These are source code, not model weights. Docker only copies these files, checks
`SHA256SUMS` and extracts them; it never clones GitHub or initializes submodules.

| Bundle | Upstream source |
| --- | --- |
| CosyVoice | https://github.com/FunAudioLLM/CosyVoice/tree/074ca6dc9e80a2f424f1f74b48bdd7d3fea531cc |
| Matcha-TTS | https://github.com/shivammehta25/Matcha-TTS/tree/dd9105b34bf2be2230f4aa1e4769fb586a3c824e |
| MuseTalk | https://github.com/TMElyralab/MuseTalk/tree/0a89dec45a0192b824e3cf4daf96c239440c5ed8 |

The maintainer prepares bundles from the corresponding GitHub codeload archives
outside the target Docker build. Top-level `asset`, `assets`, `data`, `.github`
and notebooks are excluded: these contain demo media, documentation or CI, not
runtime code. Nested tokenizer assets, code, configuration and upstream licenses
are preserved. Tar ownership and timestamps, and gzip timestamps, are normalized.
`PROVENANCE.json` records original and repacked archive checksums and exclusions.

Model checkpoints and user reference audio remain in separately configured host
mounts. Updating these bundles requires an explicit upstream revision review,
checksum update and inference acceptance; it is not a startup download.
