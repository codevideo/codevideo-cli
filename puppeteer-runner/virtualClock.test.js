const { test } = require('node:test');
const assert = require('node:assert/strict');
const puppeteer = require('puppeteer-core');
const { installVirtualClock } = require('./virtualClock');
const { resolveChromeExecutable } = require('./runtimePaths');
const { rangesFor, makeTrace } = require('./renderFramesV1');

test('worker segments cover each frame exactly once', () => {
  for (const workers of [1, 4, 8, 32]) {
    for (const count of [1, 7, 1801]) {
      const ranges = rangesFor(count, workers);
      assert.equal(ranges[0].start, 0);
      assert.equal(ranges.at(-1).end, count);
      ranges.forEach((range, i) => {
        assert.ok(range.end > range.start);
        if (i) assert.equal(range.start, ranges[i - 1].end);
      });
    }
  }
});

test('trace rejects unfinished actions', () => {
  assert.throws(() => makeTrace({ frameCount: 60, events: [{type: 'action-start', actionIndex: 0, name: 'author-wait', timestampMs: 0}] }, {}), /Incomplete/);
});

test('virtual clock drains timers, microtasks and MessageChannel; rAF uses fixed steps', { timeout: 30000 }, async () => {
  const browser = await puppeteer.launch({ executablePath: resolveChromeExecutable(), headless: true, args: ['--no-sandbox'] });
  try {
    const page = await browser.newPage();
    await page.evaluateOnNewDocument(installVirtualClock);
    await page.goto('data:text/html,<div id="box"></div>');
    const result = await page.evaluate(async () => {
      await window.__codeVideoClock.start();
      const events = [];
      const record = name => events.push([name, performance.now()]);
      const cancelled = setTimeout(() => record('cancelled'), 1); clearTimeout(cancelled);
      const channel = new MessageChannel();
      channel.port1.onmessage = () => { record('message'); document.querySelector('#box').textContent = 'committed'; };
      setTimeout(async () => {
        record('timer'); await Promise.resolve(); record('promise');
        channel.port2.postMessage('flush');
        setTimeout(() => record('nested'), 0);
      }, 10);
      let count = 0;
      const interval = setInterval(() => { record('interval'); if (++count === 2) clearInterval(interval); }, 7);
      requestAnimationFrame(t => { events.push(['raf', t]); requestAnimationFrame(t => events.push(['raf', t])); });
      await window.__seek(100);
      const snapshot = { events, now: Date.now(), value: new Date().getTime(), parsed: Date.parse('2000-01-01T00:00:00Z'), text: document.querySelector('#box').textContent, perf: performance.now() };
      try { await window.__seek(99); } catch (error) { snapshot.backwardError = error.message; }
      return snapshot;
    });
    assert.deepEqual(result.events.slice(0, 6), [['interval', 7], ['timer', 10], ['promise', 10], ['message', 10], ['nested', 10], ['interval', 14]]);
    assert.deepEqual(result.events.slice(6), [['raf', 1000 / 60], ['raf', 2000 / 60]]);
    assert.equal(result.now, 946684800100); assert.equal(result.value, result.now);
    assert.equal(result.parsed, 946684800000); assert.equal(result.perf, 100);
    assert.equal(result.text, 'committed'); assert.match(result.backwardError, /forward/);
  } finally { await browser.close(); }
});

test('offset startup and boundary handshake preserve global time and callback order', { timeout: 30000 }, async () => {
  const browser = await puppeteer.launch({ executablePath: resolveChromeExecutable(), headless: true, args: ['--no-sandbox'] });
  try {
    const page = await browser.newPage();
    await page.evaluateOnNewDocument(installVirtualClock);
    await page.goto('data:text/html,<div></div>');
    const events = await page.evaluate(async () => {
      const log = [];
      await window.__codeVideoClock.start(20040);
      requestAnimationFrame(t => log.push(['raf', t]));
      setTimeout(() => {
        log.push(['end', performance.now()]);
        window.__codeVideoClock.atBoundary(() => { log.push(['checkpoint', performance.now()]); setTimeout(() => log.push(['next', performance.now()]), 0); });
        setTimeout(() => log.push(['commit', performance.now()]), 0);
      }, 2);
      await window.__seek(20050);
      return log;
    });
    assert.deepEqual(events.slice(0, 4), [['end', 20042], ['commit', 20042], ['checkpoint', 20042], ['next', 20042]]);
    assert.ok(Math.abs(events[4][1] - 20050) < 1e-7); assert.equal(events[4][0], 'raf');
  } finally { await browser.close(); }
});
