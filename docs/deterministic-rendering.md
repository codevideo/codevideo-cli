# Experimental deterministic rendering

Realtime remains the default. Opt in locally:

```bash
./codevideo --project "$(cat actions.json)" --output out/demo.mp4 \
  --render-mode deterministic --render-seed demo-v1 --render-workers 4
```

`--render-workers` accepts 1–32 and defaults to 1. More workers are not always faster: every browser bootstraps Monaco. The default `sequential-seek` strategy replays the full timeline in every worker; the opt-in `ab-initio` strategy restores saved action boundaries. On the M3 Max golden fixture, 4 workers beat 8. Budget browser workers across jobs: `CODEVIDEO_MAX_CONCURRENT_JOBS × renderWorkers` browsers can capture simultaneously, plus planning/bootstrap overhead. Start production experiments with one job and one worker.

The manifest may contain `renderMode`, `renderSeed`, `renderWorkers`, and `renderStrategy`. Mode precedence is explicit manifest → `CODEVIDEO_RENDER_MODE` → `realtime`; the CLI resolves explicit flags before generating its manifest. Unknown modes fail. If no seed is supplied, a UUID seed is persisted into the job manifest and included in its trace. Reuse it for comparisons. Course lessons receive independent seed namespaces when a CLI seed is provided.

## Implementation and repository boundaries

| Repository | Main files | Responsibility |
| --- | --- | --- |
| codevideo-types | `src/types/RenderMode.ts`, `src/interfaces/ICodeVideoIDEProps.ts`, `src/interfaces/video/{ICodeVideoManifest,IAudioItem}.ts`, `src/index.ts` | Optional shared mode/seed/worker and prepared audio metadata; existing producers remain valid. |
| codevideo-ide-react | `src/utils/deterministicRandom.ts`, `src/rendering/types.ts`, `src/hooks/useReplayPlayback.ts`, `src/utils/executeActionPlaybackForMonacoInstance.ts` | Action-scoped random streams; trace/error callbacks; duration-based speech cues; awaited file-explorer typing. |
| codevideo-ide-react | `src/MouseOverlay/MouseOverlay.tsx`, its coordinate helpers, `src/CodeVideoIDE.tsx`, `src/Terminal/Terminal.tsx` | Seeded targets/curves, deterministic caret and scrolling, no asynchronous editor diagnostics/semantic decorations. |
| codevideo-ide-react | `example/src/pages/v3.tsx`, `scripts/golden/{embed-local.sh,benchmark.mjs,README.md}` | URL/manifest bridge, readiness/completion/error reporting, unpublished local consumption and golden validation. |
| codevideo-cli | `main.go`, `cli/config/config.go`, `cli/generator/generator.go`, `cli/cli.go`, `types/{types,renderMode,renderTrace}.go` | Flags, manifest propagation, mode validation, errors returned to CLI. |
| codevideo-cli | `server/{server,deterministic}.go`, `utils/{prepareRenderAudio,muxRenderAudio}.go`, `constants/constants.go` | Dispatch, asset materialization/probing, offline audio mux, trace/metrics, cleanup. |
| codevideo-cli | `puppeteer-runner/{virtualClock,renderFramesV1}.js`, package manifests/locks, `npm/scripts/{prepare,verify}-packages.mjs` | Virtual clock, planning pass, browser workers, FFmpeg encoding and npm runtime packaging. |
| codevideo-cli | `cli/staticserver/public/` | Generated Gatsby assets embedded in the Go executable. Must be regenerated after React changes. |
| codevideo-api | `DEPLOYMENT.md` | Release order and isolated rollout guidance. No public API behavior or active image pin changes. |

`codevideo-virtual-ide` and the legacy `recordVideoV3.js` / `ConvertToMp4` path stay as they are. Checkpoint payloads belong to the React package, not the shared types or virtual-state packages. Arbitrary seeking, distributed execution, incremental rendering and frame deduplication remain out of scope.

## How rendering works

1. Go materializes existing audio URLs or data URIs, deduplicates bytes by SHA-256, and probes durations with ffprobe. Missing assets, downloads, and probes fail the job. Indexed assets take precedence; old text-only items still work.
2. Chrome loads `/v3?renderMode=deterministic` with the manifest intercepted locally. Fonts, authored-file language support, Monaco and the application bridge must be ready before virtual time starts. The browser receives audio identity/duration, not playable media.
3. A pre-script clock controls top-frame timers, intervals, rAF, idle callbacks, Date/performance and CSS animations. It drains microtasks, React MessageChannel work and DOM mutations before capture. Seeks are sequential and forward only, in fixed 60 fps steps. The recording trigger occurs at 1 second; the existing replay hook retains its initial 1-second delay.
4. A planning replay discovers exact action, mouse and speech events. Each capture worker replays from zero, captures its assigned contiguous range, then finishes replaying to verify its complete event trace against planning. With `ab-initio`, planning also exports settled action-boundary checkpoints and workers execute only their assigned actions.
5. Screenshots are JPEG quality 92, encoded to H.264 at 60 fps, CRF 18, preset fast, yuv420p. Segment files concatenate in frame order with stream copy and faststart. A final 1-second hold follows whichever finishes later: replay or the last speech cue.
6. Speech-before/after block for measured duration; speech-during emits a nonblocking cue. Go mixes cues at 48 kHz with sample-based delays, a limiter and AAC 384 kbps, over a finite silence bed. Video is stream-copied. Silent lessons need no ffprobe and no audio mux.

The runner closes browsers/encoders on failure and bounds virtual duration to one hour and job wall time to 30 minutes. CLI errors return a nonzero status. Temporary assets and segment files are removed; final trace/metrics remain for diagnosis.

## Action-boundary checkpoint strategy

```bash
./codevideo --project "$(cat actions.json)" --output out/checkpoint.mp4 \
  --render-mode deterministic --render-strategy ab-initio \
  --render-seed demo-v1 --render-workers 4
```

Realtime remains the overall default, and `sequential-seek` remains the deterministic default. Unknown strategies fail; the CLI requires deterministic mode for `ab-initio`.

Planning captures versioned `RenderCheckpointV1` payloads from the live component after an action and its same-time updates settle, before the next action starts. The handshake does not advance virtual time. Active mouse motion, unfinished CSS animation and pending overlay view restoration exclude a boundary. External browser/web-preview actions make the entire job fall back to sequential seek with a reason in metrics.

Snapshots include committed UI state (files/tabs/terminal/captions/menus/dialogs/inputs), overlay continuation, actual Monaco models and languages, selections and view states, per-tab view caches, exact editor layout, mouse state, viewport, scroll positions and presentation deadlines. Scrollbar visibility uses a 500 ms hold and 800 ms fade on the virtual clock; restoration keeps the remaining time. Terminal caret deadlines are also recorded. Payloads live in temporary JSON files, indexed by timestamp/action and SHA-256. Project/settings/seed identity and payload integrity are checked before use; restore errors fail the job.

Workers receive structured session data before `/v3` mounts. Replay and step effects stay suspended while state, language support, font metrics, models and view states initialize. The clock then starts at the saved absolute time and the next action begins exactly once, without the initial hold. Its animation frames stay on the original global 60 fps grid. Only the first worker includes the introduction and only the final worker includes the speech tail and closing hold.

Eligible boundaries partition the timeline into contiguous ranges minimizing the largest frame count. A boundary at `t` begins at `ceil(t * 60 / 1000)`; ranges are half-open `[startFrame, endFrame)`. Fewer distinct boundaries mean fewer actual workers. Long actions are indivisible: the golden fixture spends 15.04 of its 28.65 seconds in one typing action, so adding workers cannot remove that bottleneck.

Each worker finishes its final action and stops before the next, reporting segment completion separately from project completion. Its action/mouse/speech event slice must equal planning. The authoritative full trace and audio cues come from planning. A speech cue crossing a boundary is muxed once at its original time.

Metrics include requested/effective strategy, fallback reason, requested/actual workers, eligible checkpoints, planning frames, capture frames, each segment's range and bootstrap/restore time. `evaluatedFrames` excludes planning: checkpoint capture evaluates exactly the output frame count, while sequential seek evaluates that count once per worker. `captureMs` includes bootstrap, restore, capture and segment encoding.

Browser validation can be run from the CLI repo:

```bash
CODEVIDEO_BROWSER_TESTS=1 node --test puppeteer-runner/checkpoints.integration.test.js
CODEVIDEO_BROWSER_TESTS=1 go test ./server -run TestDeterministicSpeechRender -v
```

The browser test compares every source JPEG for focused state fixtures, resuming at every eligible boundary. The speech integration uses generated local tones spanning segment boundaries and the final action. `CODEVIDEO_CHECKPOINT_TEST_OUTPUT` preserves artifacts; `CODEVIDEO_CHECKPOINT_SAVE_FRAMES=1` saves all comparison frames. The benchmark accepts `CODEVIDEO_BENCHMARK_WORKERS=1,2,4,8`, `CODEVIDEO_BENCHMARK_REPEATS=3`, and `CODEVIDEO_BENCHMARK_FIXTURES`.

Atomic typing/wait checkpoints, mathematical planning and arbitrary-frame scrubbing are deferred. Publishing follows the release order below after local validation; promoting either default is a separate rollout decision.

## Reproducibility and limitations

The output sidecars are `<stem>.render-trace.json` (versioned logical timeline, seed, action bounds, speech cues, mouse paths, frame count) and `<stem>.render-metrics.json` (planning/capture/render/audio/mux/job timings and sampled source JPEG hashes). CLI default-named outputs also receive both sidecars. `CODEVIDEO_RENDER_SAMPLE_DIR` optionally retains sampled JPEGs for visual debugging.

Pin Chrome, fonts, component build, manifest and seed when comparing pixels. Reproducibility across different browsers/OS/font rasterizers is not promised. Different worker counts change H.264 segment boundaries and can change encoded MP4 bytes even with identical source frames. Sample hashes normally check selected frames. `CODEVIDEO_RENDER_HASH_ALL=1` hashes every source frame.

Monaco syntax colors remain; asynchronous diagnostics, semantic highlights, unused/deprecated hints, inlay hints, sticky-scroll headers are omitted in deterministic mode because language workers run in wall time. The caret is solid, the overview-ruler cursor marker is hidden, and smooth scrolling is disabled. Software compositing and a consistent Monaco layout flush avoid history-dependent subpixel rasterization. External websites, executable previews and their child frames remain **best effort**: their network/worker clocks are not virtualized. There is no network/content cache, and Monaco/fonts must still load successfully.

## Validation and local integration

Use Node 20 or 22 for Gatsby. Install dependencies in types, React, React/example and CLI/puppeteer-runner first. From the React repo:

```bash
# Build local types/keyboard → React → clean Gatsby → embedded CLI. No npm publication.
bash scripts/golden/embed-local.sh
# Golden fixture, no paid TTS or Slack. quick: legacy vs ab-initio at 1/2/4/8
# workers, one run each (~3 min); full: both strategies and both typing
# profiles, three rotated runs each (54 renders).
scripts/golden/bench.sh quick
scripts/golden/bench.sh full
```

`CODEVIDEO_BENCHMARK_REFERENCE=<earlier output dir>` additionally requires every deterministic frame and the trace to match a known-good build; see `scripts/golden/README.md` for all overrides.

`CODEVIDEO_TYPES_DIR`, `CODEVIDEO_CLI_DIR`, `CODEVIDEO_BUILD_OUTPUT` and `CODEVIDEO_CLI_BINARY` support alternate sibling/build locations. The benchmark uses a fresh output directory, preserves logs/videos/sidecars and checks trace equality, sampled JPEG equality, encoded frame counts and 60 fps. It reports end-to-end wall time separately from planning/capture time. It refuses to overwrite previous results.

Checks: types build/type-check/tests; React build/type-check/Jest and Gatsby SSR build; `go test ./...`; `npm test --prefix puppeteer-runner` (Chrome required); `npm run prepare:packages --prefix npm && npm test --prefix npm`. `CODEVIDEO_BROWSER_TESTS=1 go test ./server -run TestDeterministicSpeechRender -v` exercises the full speech render without a TTS service. Go audio integration uses generated tones to check downloads/data URIs, deduplication, cue delays, overlap and the final tail without a TTS service.

## Release order

The current local workflow consumes unpublished source overlays; publishing is a separate release step:

1. Release the additive shared types changes.
2. Update React's types dependency and lockfile to that published release; run its checks, then release React.
3. Update `example/` types and React dependencies/lockfile to those releases. **Clean Gatsby's cache** (node_modules overlays can otherwise retain stale bundles), rebuild the site, copy `public/`, then rebuild/package the CLI and worker image.
4. Release the CLI npm platform packages/wrapper and Docker image together with the new JS runtime files. Use the existing release scripts; do not publish only the Go binary.
5. Update the API worker image pin only after that image exists. Keep realtime default; use explicit deterministic manifests on an isolated test queue before enabling any shared worker default.

No release versions or published lockfile integrity values are invented before those packages exist. The checked-in dependency locks still describe the existing published baseline; use `embed-local.sh` for this cross-repository feature until the ordered releases are made.
