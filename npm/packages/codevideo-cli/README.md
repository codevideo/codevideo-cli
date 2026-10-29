# CodeVideo CLI

Cross-platform npm distribution of the CodeVideo video rendering CLI.

```shell
npx @fullstackcraftllc/codevideo-cli doctor
npx @fullstackcraftllc/codevideo-cli install-browser
npx @fullstackcraftllc/codevideo-cli --version
```

Rendering requires FFmpeg on `PATH` (or `CODEVIDEO_FFMPEG_PATH`). Chrome is detected automatically; if none is installed, run `codevideo-cli install-browser`.

Experimental virtual-time rendering: pass `--render-mode deterministic`, optionally
`--render-seed my-seed --render-workers 4` (1–32 workers, default 1). Realtime is still
the default. Deterministic speech additionally requires ffprobe on PATH or
`CODEVIDEO_FFPROBE_PATH`. Trace and timing JSON files are written beside the MP4.
