const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

// puppeteer-stream's websocket server keeps Node running, so a recording that
// fails has to exit on its own; otherwise the CLI, or a worker slot, waits
// forever. It also tells the CLI why, on a CODEVIDEO_PROGRESS line.
test('a failed recording exits and reports its error', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'codevideo-recorder-'));
  try {
    const result = spawnSync(process.execPath, [
      path.join(__dirname, 'recordVideoV3.js'),
      '--uuid', 'test',
      '--output-webm', path.join(dir, 'test.webm'),
    ], {
      env: { ...process.env, CODEVIDEO_CHROME_PATH: path.join(dir, 'no-such-chrome') },
      encoding: 'utf8',
      timeout: 30000,
    });
    assert.equal(result.error, undefined, `the recorder did not exit: ${result.error}`);
    assert.equal(result.status, 1, result.stderr);
    const line = result.stdout.split('\n').find(l => l.startsWith('CODEVIDEO_PROGRESS '));
    assert.ok(line, `no CODEVIDEO_PROGRESS line in:\n${result.stdout}`);
    const event = JSON.parse(line.slice('CODEVIDEO_PROGRESS '.length));
    assert.match(event.error, /^CODEVIDEO_CHROME_PATH is not an executable file: /);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
