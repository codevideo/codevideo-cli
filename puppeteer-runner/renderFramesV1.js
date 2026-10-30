const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const { spawn } = require('node:child_process');
const { once } = require('node:events');
const puppeteer = require('puppeteer-core');
const { resolveChromeExecutable, resolveFfmpegExecutable } = require('./runtimePaths');
const { installVirtualClock } = require('./virtualClock');
const { actionSlicesFor, projectFingerprint, fallbackReason, readCheckpoint } = require('./renderSegments');
const FPS = 60;
const MAX_FRAMES = FPS * 60 * 60;

function rangesFor(frameCount, workers) {
  const count = Math.min(workers, frameCount);
  return Array.from({ length: count }, (_, i) => ({ start: Math.floor(frameCount * i / count), end: Math.floor(frameCount * (i + 1) / count) }));
}
function dimensions(resolution, orientation) {
  if (!['1080p', '4K'].includes(resolution) || !['landscape', 'portrait'].includes(orientation)) throw new Error('Invalid resolution or orientation');
  const size = resolution === '4K' ? [3840, 2160] : [1920, 1080];
  return orientation === 'portrait' ? size.reverse() : size;
}
function runFFmpeg(args, signal) {
  const child = spawn(resolveFfmpegExecutable(), ['-y', '-loglevel', 'error', ...args], { stdio: ['pipe', 'ignore', 'pipe'], signal });
  let stderr = '';
  child.stderr.on('data', data => { stderr = (stderr + data).slice(-8192); });
  child.stdin.on('error', () => {});
  const done = new Promise((resolve, reject) => {
    child.on('error', reject);
    child.on('close', code => code === 0 ? resolve() : reject(new Error(`FFmpeg exited ${code}: ${stderr}`)));
  });
  done.catch(() => {});
  return { child, done };
}
async function createScene(options) {
  const bootStarted = performance.now();
  const [width, height] = dimensions(options.resolution, options.orientation);
  const browser = await puppeteer.launch({
    executablePath: resolveChromeExecutable(), headless: !options.debug,
    protocolTimeout: 60000,
    defaultViewport: { width, height, deviceScaleFactor: 1 },
    args: ['--disable-gpu', '--no-sandbox', '--disable-dev-shm-usage', '--force-color-profile=srgb', '--font-render-hinting=none', '--disable-frame-rate-limit', '--disable-gpu-vsync', '--disable-background-timer-throttling'],
  });
  options.browsers.add(browser);
  try {
    const page = await browser.newPage();
    page.setDefaultTimeout(60000);
    await page.emulateTimezone('UTC');
    const failures = [];
    page.on('pageerror', error => failures.push(error.message));
    await page.evaluateOnNewDocument(installVirtualClock);
    if (options.renderSession) await page.evaluateOnNewDocument(session => { window.__codeVideoRenderSession = session; }, options.renderSession);
    await page.setRequestInterception(true);
    page.on('request', request => {
      const url = new URL(request.url());
      if (url.hostname === 'localhost' && url.port === '7000' && url.pathname === '/get-manifest-v3') {
        void request.respond({ status: 200, contentType: 'application/json', headers: { 'Access-Control-Allow-Origin': '*' }, body: JSON.stringify(options.manifest) });
      } else void request.continue();
    });
    await page.goto(`${options.baseUrl}/v3?uuid=${encodeURIComponent(options.manifest.uuid)}&renderMode=${options.renderMode || 'deterministic'}`, { waitUntil: 'networkidle0' });
    // Bootstrap still uses native timers. Do not poll a frozen rAF loop.
    await page.waitForFunction(() => window.__codeVideoRenderState?.ready || window.__codeVideoRenderState?.error, { polling: 50 });
    await page.evaluate(async text => {
      // fonts.ready only waits for faces used by the initial empty IDE. Tabs
      // introduce weight 500 later; loading it mid-replay changes captured pixels.
      await Promise.all([400, 500, 700].map(weight => document.fonts.load(`${weight} 26px "Fira Code"`, text)));
      await document.fonts.ready;
    }, (options.manifest.actions || options.manifest.lesson?.actions || []).map(action => action.value).join('\n') + 'CodeVideo hello.ts');
    await page.evaluate(async manifest => {
      // Syntax loaders use native promises/network work. Load languages used by
      // authored filenames (including snapshots/rename inputs) before time starts.
      const names = [];
      const visit = value => {
        if (typeof value === 'string' && value.length <= 1024) names.push(value.toLowerCase());
        else if (value && typeof value === 'object') for (const [key, child] of Object.entries(value)) { names.push(key.toLowerCase()); visit(child); }
      };
      visit(manifest);
      const languages = new Set(window.monaco.editor.getModels().map(model => model.getLanguageId()));
      for (const language of window.monaco.languages.getLanguages()) {
        if (language.extensions?.some(extension => names.some(name => name.endsWith(extension)))) languages.add(language.id);
      }
      await Promise.all([...languages].map(language => window.monaco.editor.colorize('', language, {})));
    }, options.manifest);
    await page.addStyleTag({ content: 'body { margin: 0; padding: 0; }' });
    const restoreStarted = performance.now();
    await page.evaluate(async (session, useNativeClock) => {
      const state = window.__codeVideoRenderState;
      if (state.error) throw new Error(state.error);
      if (state.version !== 3) throw new Error('Unsupported CodeVideo render bridge; rebuild the embedded Gatsby app');
      // Monaco caches font metrics even when its initial model is hidden.
      // Refresh after loading Fira Code so cold and restored editors agree.
      window.monaco.editor.remeasureFonts();
      window.__flushCodeVideoFrame = () => {
        for (const editor of window.monaco.editor.getEditors()) {
          // ResizeObserver supplies fractional content boxes; explicit layout
          // rounds clientWidth. Use the same dimensions after either startup.
          const rect = editor.getContainerDomNode().getBoundingClientRect();
          const size = {width: Math.floor(rect.width), height: Math.floor(rect.height)};
          const previous = editor.getLayoutInfo();
          if (rect.width && rect.height && (previous.width !== size.width || previous.height !== size.height)) editor.layout(size);
          editor.render(true);
        }
      };
      if (session?.checkpoint) {
        await window.__restoreCodeVideoCheckpoint();
        // Drain native Monaco layout/scroll work while replay is suspended.
        // These callbacks must not create fresh presentation deadlines later.
        await new Promise(requestAnimationFrame);
        await new Promise(requestAnimationFrame);
      }
      if (!useNativeClock) await window.__codeVideoClock.start(session?.checkpoint?.timestampMs ?? 0);
      if (session?.checkpoint) {
        await window.__codeVideoClock.settle();
        window.__startRecording();
        await window.__seek(session.checkpoint.timestampMs);
      }
    }, options.renderSession, options.useNativeClock);
    return { browser, page, failures, bootstrapMs: restoreStarted - bootStarted, restoreMs: performance.now() - restoreStarted };
  } catch (error) { options.browsers.delete(browser); await browser.close(); throw error; }
}
async function replay(options, range, outputPath, expected) {
  const scene = await createScene(options);
  const events = [], hashes = [], checkpoints = [];
  const sliced = options.strategy === 'ab-initio' && range;
  const partial = sliced && range.end < expected.frameCount;
  let encoder, endFrame = partial ? range.end : expected?.frameCount ?? MAX_FRAMES;
  let captured = 0, evaluatedFrames = 0, completionMs = null;
  const collect = state => {
    if (state.error || scene.failures.length) throw new Error(state.error || scene.failures.join('\n'));
    events.push(...state.events);
    for (const checkpoint of state.checkpoints || []) {
      const raw = JSON.stringify(checkpoint);
      const file = path.join(options.checkpointDir, `${checkpoints.length}.json`);
      fs.writeFileSync(file, raw);
      checkpoints.push({ path: file, sha256: crypto.createHash('sha256').update(raw).digest('hex'), cursor: checkpoint.cursor, tracePosition: checkpoint.tracePosition, timestampMs: checkpoint.timestampMs });
    }
  };
  try {
    if (range) encoder = runFFmpeg(['-f', 'image2pipe', '-framerate', String(FPS), '-i', 'pipe:0', '-an', '-c:v', 'libx264', '-threads', String(options.encoderThreads), '-preset', 'fast', '-crf', '18', '-pix_fmt', 'yuv420p', '-movflags', '+faststart', outputPath], options.signal);
    for (let frame = sliced ? range.start : 0; frame < endFrame; frame++) {
      if (options.signal.aborted) throw new Error('Render cancelled');
      const state = await scene.page.evaluate(async ({ timestamp, start }) => {
        await window.__seek(timestamp);
        if (start) { window.__startRecording(); await window.__seek(timestamp); }
        window.__flushCodeVideoFrame();
        await window.__seek(timestamp);
        return { ...window.__codeVideoRenderState, events: window.__drainCodeVideoRenderEvents(), checkpoints: window.__drainCodeVideoRenderCheckpoints?.() };
      }, { timestamp: frame * 1000 / FPS, start: frame === FPS && !options.renderSession?.checkpoint });
      evaluatedFrames++;
      if (options.progress && !range) options.progress.frame = frame + 1;
      if (options.debugProgress && frame % 300 === 0) console.log(JSON.stringify({frame, action: state.currentActionIndex, complete: state.complete, checkpointCount: checkpoints.length}));
      collect(state);
      if (state.complete && completionMs === null) {
        completionMs = frame * 1000 / FPS;
        // Planning owns all speech cues, including narration begun in another segment.
        const speechEnd = (expected?.events || events).reduce((end, e) => e.type === 'speech' ? Math.max(end, e.timestampMs + e.durationMs) : end, 0);
        const requiredFrames = Math.ceil((Math.max(completionMs, speechEnd) + 1000) * FPS / 1000);
        if (expected && requiredFrames !== expected.frameCount) throw new Error(`Worker timeline diverged: ${requiredFrames} frames, expected ${expected.frameCount}`);
        endFrame = requiredFrames;
      }
      if (range && frame >= range.start && frame < range.end) {
        const jpeg = await scene.page.screenshot({ type: 'jpeg', quality: 92, optimizeForSpeed: true });
        if (!encoder.child.stdin.write(jpeg)) await Promise.race([once(encoder.child.stdin, 'drain'), encoder.done.then(() => { throw new Error('Encoder closed before all frames were written'); })]);
        if (options.hashAllFrames || frame % 120 === 0 || frame === range.start || frame === range.end - 1) {
          hashes.push({ frame, sha256: crypto.createHash('sha256').update(jpeg).digest('hex') });
          if (options.sampleDir) fs.writeFileSync(path.join(options.sampleDir, `${frame}.jpg`), jpeg);
        }
        captured++;
        if (options.progress) options.progress.frame++;
      }
    }
    if (partial) {
      // Settle to the selected cursor without executing the next primitive/action.
      const state = await scene.page.evaluate(async timestamp => {
        await window.__seek(timestamp);
        return { ...window.__codeVideoRenderState, events: window.__drainCodeVideoRenderEvents() };
      }, range.endMs);
      collect(state);
      if (!state.segmentComplete) throw new Error('Worker did not stop at its execution cursor');
    } else if (completionMs === null) throw new Error('Playback exceeded the one-hour virtual duration limit');
    if (expected) {
      const expectedEvents = sliced ? expected.events.slice(range.traceStart, range.traceEnd) : expected.events;
      if (JSON.stringify(events) !== JSON.stringify(expectedEvents)) {
        const mismatch = events.findIndex((e, i) => JSON.stringify(e) !== JSON.stringify(expectedEvents[i]));
        throw new Error(`Worker trace diverged at event ${mismatch}: ${JSON.stringify(events[mismatch])}; expected ${JSON.stringify(expectedEvents[mismatch])} (${events.length}/${expectedEvents.length} events)`);
      }
      if (captured !== range.end - range.start) throw new Error('Worker captured an incorrect frame count');
    }
    if (encoder) { encoder.child.stdin.end(); await encoder.done; }
    return { events, frameCount: expected?.frameCount ?? endFrame, captured, hashes, checkpoints, evaluatedFrames, bootstrapMs: scene.bootstrapMs, restoreMs: scene.restoreMs };
  } finally {
    if (encoder && encoder.child.exitCode === null) { encoder.child.stdin.destroy(); encoder.child.kill('SIGKILL'); }
    if (encoder) await encoder.done.catch(() => {});
    options.browsers.delete(scene.browser);
    await scene.browser.close();
  }
}
function makeTrace(result, manifest) {
  const actions = [];
  for (const event of result.events) {
    if (event.type === 'action-start') actions.push({ actionIndex: event.actionIndex, name: event.name, startMs: event.timestampMs, endMs: null });
    if (event.type === 'action-end') { const action = actions.find(a => a.actionIndex === event.actionIndex); if (!action) throw new Error('Action end without start'); action.endMs = event.timestampMs; }
  }
  if (actions.some(a => a.endMs === null)) throw new Error('Incomplete action trace');
  return {
    version: 1, renderMode: 'deterministic', fps: FPS, renderSeed: manifest.renderSeed,
    frameCount: result.frameCount, durationMs: result.frameCount * 1000 / FPS,
    actions,
    speechCues: result.events.filter(e => e.type === 'speech').map(e => ({ actionIndex: e.actionIndex, type: e.name, text: e.text, startMs: e.timestampMs, durationMs: e.durationMs, assetId: e.assetId })),
    mousePaths: result.events.filter(e => e.type === 'mouse-path'),
  };
}
async function render(options) {
  const started = performance.now();
  const requestedStrategy = options.strategy || options.manifest.renderStrategy || 'sequential-seek';
  if (!['ab-initio', 'sequential-seek'].includes(requestedStrategy)) throw new Error('Invalid render strategy');
  const fallback = requestedStrategy === 'ab-initio' ? fallbackReason(options.manifest) : null;
  options.strategy = fallback ? 'sequential-seek' : requestedStrategy;
  const fingerprint = projectFingerprint(options.manifest, options.resolution, options.orientation);
  options.browsers = new Set();
  const controller = new AbortController(); options.signal = controller.signal;
  const timeout = setTimeout(() => { controller.abort(); for (const browser of options.browsers) void browser.close(); }, options.timeoutMs ?? 1800000);
  const cancel = () => { controller.abort(); for (const browser of options.browsers) void browser.close(); };
  process.once('SIGTERM', cancel); process.once('SIGINT', cancel);
  const output = path.resolve(options.output);
  if (options.sampleDir) fs.mkdirSync(options.sampleDir, { recursive: true });
  fs.mkdirSync(path.dirname(output), { recursive: true });
  const temp = fs.mkdtempSync(path.join(path.dirname(output), '.codevideo-frames-'));
  // With reportProgress (--progress), the CLI reads frame counts for its bar.
  // Planning is reported from its first frame, after the browser has started.
  const progress = { stage: 'planning', frame: 0 };
  const reportProgress = () => { if (progress.stage !== 'planning' || progress.frame > 0) console.log('CODEVIDEO_PROGRESS ' + JSON.stringify(progress)); };
  const ticker = options.reportProgress ? setInterval(reportProgress, 250) : null;
  if (options.reportProgress) options.progress = progress;
  try {
    console.log(`Deterministic renderer: planning timeline (${options.strategy})${fallback ? ': ' + fallback : ''}`);
    const checkpointDir = path.join(temp, 'checkpoints'); fs.mkdirSync(checkpointDir);
    const plan = await replay({ ...options, checkpointDir, renderSession: options.strategy === 'ab-initio' ? { kind: 'plan', fingerprint } : undefined });
    if (options.onPlan) await options.onPlan(plan);
    const plannedAt = performance.now();
    const ranges = options.strategy === 'ab-initio' ? actionSlicesFor(plan, options.workers) : rangesFor(plan.frameCount, options.workers);
    console.log(`Deterministic renderer: capturing ${plan.frameCount} frames with ${ranges.length} worker(s)`);
    Object.assign(progress, { stage: 'frames', frame: 0, frames: plan.frameCount });
    options.encoderThreads = Math.max(1, Math.min(4, Math.floor(os.availableParallelism() / ranges.length)));
    // Validate all selected checkpoint files before launching any capture workers.
    const sessions = ranges.map(range => options.strategy === 'ab-initio' ? {
      kind: 'segment', fingerprint, checkpoint: range.checkpoint ? readCheckpoint(range.checkpoint, fingerprint) : undefined,
      endCursor: range.endCursor,
    } : undefined);
    const jobs = ranges.map((range, i) => replay({ ...options, renderSession: sessions[i] }, range, path.join(temp, `${i}.mp4`), plan));
    const results = await Promise.all(jobs.map(job => job.catch(error => { cancel(); throw error; }))).catch(async error => { await Promise.allSettled(jobs); throw error; });
    const capturedAt = performance.now();
    if (ticker) reportProgress();
    // Relative, generated names avoid quoting user paths in the concat list.
    const listPath = path.join(temp, 'segments.txt');
    fs.writeFileSync(listPath, ranges.map((_, i) => `file '${i}.mp4'`).join('\n'));
    const concat = runFFmpeg(['-f', 'concat', '-safe', '1', '-i', listPath, '-c', 'copy', '-movflags', '+faststart', output], options.signal);
    concat.child.stdin.end(); await concat.done;
    const trace = makeTrace(plan, options.manifest);
    fs.writeFileSync(options.trace, JSON.stringify(trace, null, 2));
    const ended = performance.now();
    const metrics = { requestedStrategy, strategy: options.strategy, fallbackReason: fallback, requestedWorkers: options.workers,
      planningEvaluatedFrames: plan.evaluatedFrames, evaluatedFrames: results.reduce((sum, r) => sum + r.evaluatedFrames, 0),
      eligibleCheckpoints: plan.checkpoints.length,
      segments: results.map((r, i) => ({ startFrame: ranges[i].start, endFrame: ranges[i].end, startAction: ranges[i].startAction, endActionExclusive: ranges[i].endActionExclusive, startCursor: ranges[i].startCursor, endCursor: ranges[i].endCursor, traceStart: ranges[i].traceStart, traceEnd: ranges[i].traceEnd, evaluatedFrames: r.evaluatedFrames, bootstrapMs: r.bootstrapMs, restoreMs: r.restoreMs })),
      workers: ranges.length, fps: FPS, frameCount: trace.frameCount, durationMs: trace.durationMs, planningMs: plannedAt - started, captureMs: capturedAt - plannedAt, renderMs: ended - started, averageCaptureFps: trace.frameCount / ((capturedAt - plannedAt) / 1000), speedMultiplier: trace.durationMs / (ended - started), sampledFrames: results.flatMap(r => r.hashes).sort((a,b) => a.frame - b.frame), chrome: resolveChromeExecutable(), platform: `${process.platform}/${process.arch}` };
    fs.writeFileSync(options.metrics, JSON.stringify(metrics, null, 2));
    console.log(JSON.stringify({ ...metrics, sampledFrames: undefined }));
    return { trace, metrics };
  } catch (error) {
    console.error('Render failed before cleanup:', error);
    throw error;
  } finally {
    clearTimeout(timeout); clearInterval(ticker); process.removeListener('SIGTERM', cancel); process.removeListener('SIGINT', cancel);
    await Promise.allSettled([...options.browsers].map(browser => browser.close()));
    fs.rmSync(temp, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
  }
}
if (require.main === module) {
  const argv = require('yargs/yargs')(process.argv.slice(2)).strict().options({
    'manifest-path': { type: 'string', demandOption: true }, 'output-mp4': { type: 'string', demandOption: true },
    'trace-path': { type: 'string', demandOption: true }, 'metrics-path': { type: 'string', demandOption: true },
    resolution: { type: 'string', default: '1080p' }, orientation: { type: 'string', default: 'landscape' },
    strategy: { type: 'string', choices: ['sequential-seek', 'ab-initio'] },
    workers: { type: 'number', default: 1 }, debug: { type: 'boolean', default: false }, progress: { type: 'boolean', default: false },
    'base-url': { type: 'string', default: 'http://localhost:7001' },
  }).parseSync();
  if (!Number.isInteger(argv.workers) || argv.workers < 1 || argv.workers > 32) throw new Error('workers must be an integer from 1 to 32');
  const manifest = JSON.parse(fs.readFileSync(argv.manifestPath, 'utf8'));
  render({ manifest, strategy: argv.strategy, hashAllFrames: process.env.CODEVIDEO_RENDER_HASH_ALL === '1', output: argv.outputMp4, trace: argv.tracePath, metrics: argv.metricsPath, workers: argv.workers, resolution: argv.resolution, orientation: argv.orientation, debug: argv.debug, reportProgress: argv.progress, baseUrl: argv.baseUrl, sampleDir: process.env.CODEVIDEO_RENDER_SAMPLE_DIR }).catch(error => {
    console.error(error);
    // The CLI shows this message when the render fails.
    if (argv.progress) console.log('CODEVIDEO_PROGRESS ' + JSON.stringify({ error: String(error?.message || error) }));
    process.exitCode = 1;
  });
}
module.exports = { render, rangesFor, makeTrace, createScene, replay };
