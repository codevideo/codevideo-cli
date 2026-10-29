const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');
const { actionSlicesFor, frameAtOrAfter, fallbackReason, readCheckpoint, projectFingerprint } = require('./renderSegments');

function plan(frames) {
  let frame = 0;
  const checkpoints = frames.slice(0, -1).map((n, i) => ({ timestampMs: (frame += n) * 1000 / 60, cursor: { actionIndex: i + 1, timestampMs: frame * 1000 / 60, continuation: null }, tracePosition: i + 1 }));
  return { frameCount: frames.reduce((a, b) => a + b, 0), checkpoints, events: frames.map((_, actionIndex) => ({ type: 'action-start', actionIndex })) };
}
function optimal(weights, k) {
  if (k === 1) return weights.reduce((a, b) => a + b, 0);
  let best = Infinity, sum = 0;
  for (let i = 1; i <= weights.length - k + 1; i++) {
    sum += weights[i - 1];
    best = Math.min(best, Math.max(sum, optimal(weights.slice(i), k - 1)));
  }
  return best;
}
test('action partitions have optimal largest segment and exact frame coverage', () => {
  for (const weights of [[12, 900, 37, 40, 3], [1, 1, 1], [200], [30, 47, 16, 27, 100, 3]]) {
    for (const workers of [1, 2, 4, 8, 12, 32]) {
      const p = plan(weights), slices = actionSlicesFor(p, workers);
      assert.equal(slices.length, Math.min(workers, weights.length));
      assert.equal(slices[0].start, 0); assert.equal(slices.at(-1).end, p.frameCount);
      assert.equal(Math.max(...slices.map(s => s.end - s.start)), optimal(weights, slices.length));
      slices.forEach((s, i) => { if (i) { assert.equal(s.start, slices[i - 1].end); assert.equal(s.startAction, slices[i - 1].endActionExclusive); } });
      assert.deepEqual(actionSlicesFor(p, workers), slices);
    }
  }
});
test('no eligible boundary uses one worker; off-grid boundaries use ceiling', () => {
  const p = plan([20, 30]); p.checkpoints = [];
  assert.equal(actionSlicesFor(p, 8).length, 1);
  assert.equal(frameAtOrAfter(20040), 1203);
  assert.equal(frameAtOrAfter(20000), 1200);
});
test('external content falls back; fingerprints ignore worker count', () => {
  assert.match(fallbackReason({ lesson: { actions: [{ name: 'external-web-preview' }] } }), /cannot be restored/);
  assert.equal(fallbackReason({ actions: [{ name: 'editor-type' }] }), null);
  assert.equal(projectFingerprint({renderWorkers: 1, renderSeed: 's'}, '1080p', 'landscape'), projectFingerprint({renderWorkers: 8, renderStrategy: 'ab-initio', renderSeed: 's'}, '1080p', 'landscape'));
});
test('checkpoint files reject corruption and incompatible identity', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'codevideo-checkpoint-test-'));
  try {
    const checkpoint = { version: 2, fingerprint: 'f', cursor: { actionIndex: 2, timestampMs: 123.4, continuation: null }, tracePosition: 4, timestampMs: 123.4 };
    const raw = JSON.stringify(checkpoint), file = path.join(dir, '2.json'); fs.writeFileSync(file, raw);
    const entry = { path: file, sha256: crypto.createHash('sha256').update(raw).digest('hex'), cursor: checkpoint.cursor, tracePosition: 4, timestampMs: 123.4 };
    assert.deepEqual(readCheckpoint(entry, 'f'), checkpoint);
    assert.throws(() => readCheckpoint(entry, 'other'), /Incompatible/);
    fs.appendFileSync(file, ' '); assert.throws(() => readCheckpoint(entry, 'f'), /Corrupted/);
  } finally { fs.rmSync(dir, { recursive: true, force: true }); }
});
