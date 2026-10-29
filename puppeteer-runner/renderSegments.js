const fs = require('node:fs');
const crypto = require('node:crypto');

const frameAtOrAfter = ms => Math.ceil(ms * 60 / 1000 - 1e-7);

function projectFingerprint(manifest, resolution, orientation) {
  // Worker count/strategy/job identity do not affect pixels or checkpoint state.
  const { renderWorkers, renderStrategy, uuid, ...content } = manifest;
  return crypto.createHash('sha256').update(JSON.stringify({ version: 2, content, resolution, orientation })).digest('hex');
}
function fallbackReason(manifest) {
  const actions = manifest.actions || manifest.lesson?.actions || [];
  return actions.some(action => action.name.startsWith('external-'))
    ? 'External browser/web-preview state cannot be restored from a checkpoint' : null;
}
function readCheckpoint(entry, fingerprint) {
  const raw = fs.readFileSync(entry.path);
  if (crypto.createHash('sha256').update(raw).digest('hex') !== entry.sha256) throw new Error('Corrupted render checkpoint');
  const checkpoint = JSON.parse(raw);
  if (checkpoint.version !== 2 || checkpoint.fingerprint !== fingerprint || JSON.stringify(checkpoint.cursor) !== JSON.stringify(entry.cursor) || checkpoint.tracePosition !== entry.tracePosition || checkpoint.timestampMs !== entry.timestampMs) throw new Error('Incompatible render checkpoint');
  return checkpoint;
}

/** Minimax contiguous partition: greedy maximal prefixes, then earliest longest splits. */
function actionSlicesFor(plan, workers) {
  if (!Number.isInteger(workers) || workers < 1 || workers > 32) throw new Error('Invalid worker count');
  const lastAction = plan.events.filter(e => e.type === 'action-start').length;
  const points = [{ frame: 0, cursor: { actionIndex: 0, timestampMs: 0, continuation: null }, tracePosition: 0, timestampMs: 0 }];
  for (const checkpoint of [...(plan.checkpoints || [])].sort((a, b) => a.timestampMs - b.timestampMs || a.tracePosition - b.tracePosition)) {
    const frame = frameAtOrAfter(checkpoint.timestampMs);
    if (frame > points.at(-1).frame && frame < plan.frameCount && checkpoint.cursor.actionIndex < lastAction) points.push({ ...checkpoint, frame });
  }
  points.push({ frame: plan.frameCount, cursor: null, tracePosition: plan.events.length, timestampMs: plan.frameCount * 1000 / 60 });
  const count = Math.min(workers, points.length - 1);
  const greedy = limit => {
    const cuts = [0]; let current = 0;
    while (current < points.length - 1) {
      let next = current + 1;
      while (next + 1 < points.length && points[next + 1].frame - points[current].frame <= limit) next++;
      if (points[next].frame - points[current].frame > limit) return null;
      cuts.push(next); current = next;
    }
    return cuts;
  };
  let lo = Math.max(...points.slice(1).map((point, i) => point.frame - points[i].frame)), hi = plan.frameCount;
  while (lo < hi) {
    const middle = Math.floor((lo + hi) / 2), cuts = greedy(middle);
    if (cuts && cuts.length - 1 <= count) hi = middle; else lo = middle + 1;
  }
  const cuts = greedy(lo);
  while (cuts.length - 1 < count) {
    // Split the longest splittable segment; earliest midpoint wins ties.
    let choice = null;
    for (let i = 0; i + 1 < cuts.length; i++) {
      if (cuts[i + 1] - cuts[i] < 2) continue;
      const length = points[cuts[i + 1]].frame - points[cuts[i]].frame;
      if (!choice || length > choice.length) choice = { i, length };
    }
    const a = cuts[choice.i], b = cuts[choice.i + 1], midpoint = (points[a].frame + points[b].frame) / 2;
    let split = a + 1;
    for (let j = a + 2; j < b; j++) if (Math.abs(points[j].frame - midpoint) < Math.abs(points[split].frame - midpoint)) split = j;
    cuts.splice(choice.i + 1, 0, split);
  }
  return cuts.slice(0, -1).map((index, i) => {
    const start = points[index], end = points[cuts[i + 1]];
    return { start: start.frame, end: end.frame, startAction: start.cursor?.actionIndex ?? 0, endActionExclusive: end.cursor?.actionIndex ?? lastAction, startCursor: start.cursor, endCursor: end.cursor, traceStart: start.tracePosition, traceEnd: end.tracePosition, startMs: start.timestampMs, endMs: end.timestampMs, checkpoint: index ? start : null };
  });
}
module.exports = { actionSlicesFor, projectFingerprint, fallbackReason, frameAtOrAfter, readCheckpoint };
