# codevideo

The CLI tool for generating CodeVideos.

## Experimental deterministic rendering

Realtime remains the default. To render with virtual time and parallel browsers:

```shell
./codevideo --project "$(cat actions.json)" --output demo.mp4 \
  --render-mode deterministic --render-seed demo-v1 --render-workers 4
```

Workers default to 1 (allowed: 1–32). Speech uses existing audio assets and requires
ffprobe on PATH or `CODEVIDEO_FFPROBE_PATH`. The output includes trace/metrics JSON
sidecars. Add `--render-strategy ab-initio` to restore action-boundary checkpoints instead of replaying earlier frames in each worker. Sequential seek remains the deterministic default. See [implementation, local benchmark and release order](docs/deterministic-rendering.md).

## npm installation

The npm wrapper installs only the native binary for your operating system and architecture:

```shell
npx @fullstackcraftllc/codevideo-cli doctor
npx @fullstackcraftllc/codevideo-cli install-browser # only when Chrome is not already installed
npx @fullstackcraftllc/codevideo-cli --version
```

Rendering requires Node.js 20 or newer, Chrome/Chromium, and FFmpeg. Chrome is detected from `CODEVIDEO_CHROME_PATH`, the managed CodeVideo browser cache, or common system locations. FFmpeg is resolved from `CODEVIDEO_FFMPEG_PATH` and then `PATH`.

The wrapper supports these optional runtime locations:

- `CODEVIDEO_PUPPETEER_RUNNER_PATH`
- `CODEVIDEO_WORK_DIR`
- `CODEVIDEO_LOG_DIR`
- `CODEVIDEO_OUTPUT_DIR`
- `CODEVIDEO_BROWSER_CACHE_DIR`

## Source installation

```shell
git clone https://github.com/codevideo/codevideo
cd codevideo
cd puppeteer-runner
npm install
npx @puppeteer/browsers install chrome@latest
```

Make a note of what was installed in `puppeteer-runner/chrome` - you'll need to update the path in `puppeteer-runner/recordVideoV3.js` on line 73.

Then build the Go binary:

```shell
cd ../..
go build -o codevideo
```

Very importantly, create a `.env` file with your Elevenlabs API key:

```env
# Copy from .env.example and fill in your values
ELEVEN_LABS_API_KEY=your-elevenlabs-api-key
# ... other configuration options
```

You should be ready to start using the CodeVideo CLI!

If you don't have an Elevenlabs account - we're working on a solution with htgo-tts and other providers.

## Usage

With actions:

```shell
./codevideo -p "$(cat data/actions.json)"
```

Note: if you are using `zsh` and get the error `zsh: event not found: \`, try deactivating history expansion with `set +o histexpand` and try the command again.

While it renders, one progress bar updates in place with the current stage, the time so far and the estimated total:

```shell
Detected project type: Actions
/> CodeVideo generation in progress...
[==========                ]  38%  Recording · action 4/12 · 0:42 / 1:50
```

If all works well, you should see the following final output:

```shell
Detected project type: Actions
/> CodeVideo generation in progress...
[==========================] 100%  Done · 1:52
✅ CodeVideo successfully generated and saved to CodeVideo-2025-03-21-18-58-47.mp4
```

As an alternative, paste your actions, lesson, or course JSON into `data/actions.json`, `data/lesson.json`, or `data/course.json` respectively - all types are accepted.

With actions:

```shell
./codevideo -p "$(cat data/actions.json)"
```

With a lesson:

```shell
./codevideo -p "$(cat data/lesson.json)"
```

With a course:

```shell
./codevideo -p "$(cat data/course.json)"
```

## Complex CLI Example - Actions, With Given Output Path, and Open when Done

```shell
./codevideo -p "$(cat data/actions.json)" -o codevideo-intro.mp4 --open
```

## Output and logging

By default the CLI shows the progress bar, any warnings or errors, and the saved video. Everything else goes to the log file (`codevideo.log` in `CODEVIDEO_LOG_DIR`; next to the binary for source installs), which always records debug detail.

| Flag | Effect |
| --- | --- |
| `-v`, `--verbose` | Also show info log lines (config, TTS provider, log file path) above the bar |
| `--log-level <level>` | Show log lines down to `error`, `warn`, `info`, `debug` or `trace`; overrides `-v` |
| `-q`, `--quiet` | Hide the bar and status lines; errors and the saved path still print |
| `--headed` | Show the browser window instead of running it headless (replaces `-d`/`--debug`, which still works with a deprecation notice) |
| `--slack-webhook-url <url>` | Post progress to a Slack incoming webhook at each stage and every 25% |

The bar, status and log lines go to stderr; the "saved to" line goes to stdout, one per video. When stderr isn't a terminal (CI, pipes, `TERM=dumb`), the bar is replaced by a plain line at each stage and every 25%:

```shell
[ 52%] Recording · action 1/3 · 0:11 / 0:21
```

A course shows an overall bar with one bar per lesson below it. Every lesson is narrated first, then each is rendered in turn:

```shell
[============              ]  48%  Starting browser · lesson 2/2 · 0:23 / 0:47
  1 Variables      [================] 100%  Done
  2 Printing valu… [=               ]  12%  Starting browser
```

The estimate comes from the actions themselves (typing speed, pauses, narration length), using the measured length of each narration clip once it's generated, and is refined from the browser, ffmpeg and frame capture rates as the render runs. In serve mode the worker keeps logging everything to stdout and posting to `CODEVIDEO_SLACK_WEBHOOK_URL`.

## Video Configuration Options

You can specify the orientation and resolution of the video with the `-r` or `--resolution` and `-n` or `--orientation` flags, respectively. The default resolution is `1080p` and the default orientation is `landscape`.

## IDE Configuration Options

All React IDE props from the `CodeVideoIDE` can be passed in via the `-c` or `--config` to a config.json file. (See `data/config.json` for an example)

```shell
./codevideo -p "$(cat data/actions.json)" -c data/config.json
```

## Server usage:

Simply pass the `-m serve` parameter to the command to start the server:

```shell
./codevideo -m serve
```

To run in the background use `nohup` or similar:

```shell
nohup ./codevideo -m serve &
```

This will watch for manifest files in /tmp/v3/new and process them as they arrive. The server will output the video to the `output` folder.

## Docker

The render worker ships as a multi-arch image (`linux/amd64`, `linux/arm64`) on
Docker Hub as `fullstackcraft/codevideo-cli`. CI builds and pushes it on every `v*`
tag — see `.github/workflows/docker-image.yml`.

### As the API render worker (serve mode)

The [codevideo-api](https://github.com/codevideo/codevideo-api) compose stack pulls
this image and runs it in serve mode. It's a filesystem-queue worker — no published
port — that shares the API's render queue through a bind mount and reads its secrets
(S3, Clerk, Slack, Mailjet) from the central `.env`:

```yaml
  codevideo-cli:
    image: fullstackcraft/codevideo-cli:0.0.12   # pin to a release
    restart: always
    init: true
    shm_size: "1gb"
    volumes:
      - ./tmp/v3/:/work/tmp/v3/
    env_file: .env
    environment:
      - CODEVIDEO_WORK_DIR=/work/tmp/v3
```

### Build it yourself

```shell
docker build -t fullstackcraft/codevideo-cli:dev .
```

### One-off CLI render (no server)

Mount an env file and an output dir, then pass actions directly (this overrides the
default `-m serve`):

```shell
docker run --rm \
  -e CODEVIDEO_OUTPUT_DIR=/app/output \
  -v "$(pwd)/.env:/app/.env" \
  -v "$(pwd)/output:/app/output" \
  fullstackcraft/codevideo-cli:dev \
  -p "[{\"name\":\"author-speak-before\",\"value\":\"Let's learn how to use the print function in Python!\"}]"
```

## For Developers

You can update the Gatsby static site by replacing the `public` folder within `cli/staticserver`. We recommend you use the `example` site within the `example` folder of the [`@fullstackcraftllc/codevideo-ide-react`](https://github.com/codevideo/codevideo-ide-react) repository.

Everything in the `public` folder is treated as an embedded Go resource and served by the server.

### Tests

| Layer | Covers | Run |
| --- | --- | --- |
| Unit | Progress model, estimator, flags and log levels, Slack, runner and ffmpeg output parsing | `go test ./...` |
| Golden screens | What the renderer draws on a terminal and in plain output (`cli/renderer/testdata`) | `go test ./cli/renderer`, with `-update` to accept new output |
| CLI end to end | The real binary against fake `node`, `ffmpeg` and `ffprobe`: flags, stdout/stderr, exit codes, Slack posts, and the screen on a pseudo-terminal (ConPTY on Windows). Scripts are in `testdata/script` | `go test -run TestScripts .`, with `-update` to rewrite the golden screens in the scripts |
| Node runners | A failed recording exits and says why | `cd puppeteer-runner && npm ci && npm test` |
| Browser | A deterministic render with speech, on real Chrome and ffmpeg | `CODEVIDEO_BROWSER_TESTS=1 go test ./server -run TestDeterministicSpeechRender` |
| Browser selection and checkpoints | Monaco selection and portrait layout in realtime/deterministic replay; uninterrupted and restored-worker render frames | `cd puppeteer-runner && CODEVIDEO_BROWSER_TESTS=1 CODEVIDEO_SELECTION_RENDER=1 CODEVIDEO_CHECKPOINT_TEST_WORKERS=2 npm run test:browser` |
| Docker | Serve-mode jobs and CLI renders in the worker image | `.github/workflows/docker-image.yml` |

CI runs the first four on Ubuntu, macOS and Windows for every pull request, the browser tests on Ubuntu, and the Docker renders on pushes to main and release tags. The JavaScript browser tests use committed fixtures in `puppeteer-runner/fixtures` and a published VirtualIDE dev dependency; no sibling checkout is needed. Install runner dependencies with `npm ci` first, and provide Chrome and FFmpeg (or set `CODEVIDEO_CHROME_PATH` and `CODEVIDEO_FFMPEG_PATH`). Browser test files run sequentially to limit Chrome/encoding resource usage. The estimator's fixture (`cli/estimate/testdata/estimate-cases.json`) is shared with codevideo-types, whose CI fails if the two copies differ. The end-to-end scripts run one at a time because every CLI run binds port 7001; running them in parallel would need a port override.

## CodeVideo Studio

Build your actions JSON in the [CodeVideo Studio](https://studio.codevideo.io)!

## Deployment

Set `NPM_TOKEN` as a repository variable in Github.

Then run the following command to deploy:

```shell
go test ./...

cd npm
npm ci
npm run release:check
```

```shell
git tag -a v0.0.7 -m "Release codevideo-cli 0.0.7"
git push origin main
git push origin v0.0.7
```

### Portrait editor layout

Use `--orientation portrait --config examples/portrait.config.json` with your
normal project arguments. The config hides the file explorer, editor tabs (and
filenames), and minimap, and enables visual word wrap. Each visibility option is
independent and defaults to `true`; `wordWrap` defaults to `"on"` and also accepts
`"off"`. Wrapping does not modify the source text. Explicit `false` survives config
loading; omitted layout fields retain the React defaults. No separate CLI flags
are needed for these options.
