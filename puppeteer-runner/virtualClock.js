/* Installed before application scripts. Bootstrap runs normally; start() moves all
 * tracked work onto a fixed epoch. Only the main frame is controlled: external
 * pages/previews are explicitly best effort. */
function installVirtualClock() {
  if (window !== window.top) return;
  const native = {
    Date, now: performance.now.bind(performance), timeout: setTimeout.bind(window),
    clear: clearTimeout.bind(window), raf: requestAnimationFrame.bind(window),
    cancelRaf: cancelAnimationFrame.bind(window), Channel: MessageChannel,
    idle: window.requestIdleCallback?.bind(window), cancelIdle: window.cancelIdleCallback?.bind(window),
  };
  const epoch = 946684800000;
  const step = 1000 / 60;
  let active = false, now = 0, id = 0, frame = 1, messages = 0, revision = 0;
  const tasks = new Map(), rafs = new Map(), animations = new WeakMap();
  const boundaries = [];
  const checkpoint = new native.Channel();
  const checkpoints = [];
  checkpoint.port1.onmessage = () => checkpoints.shift()?.();
  const yieldTask = () => new Promise(resolve => { checkpoints.push(resolve); checkpoint.port2.postMessage(null); });
  new MutationObserver(() => { revision++; }).observe(document, { subtree: true, childList: true, attributes: true, characterData: true });

  // Keep real transferable ports, but account for scheduler messages while
  // draining React. A native task boundary also drains promise continuations.
  window.MessageChannel = function () {
    const channel = new native.Channel();
    for (const [from, to] of [[channel.port1, channel.port2], [channel.port2, channel.port1]]) {
      const post = from.postMessage.bind(from);
      from.postMessage = (...args) => { messages++; try { return post(...args); } catch (e) { messages--; throw e; } };
      to.addEventListener('message', () => { messages--; revision++; });
    }
    return channel;
  };
  window.MessageChannel.prototype = native.Channel.prototype;
  const settle = async () => {
    let quiet = 0;
    for (let i = 0; i < 100; i++) {
      const before = revision;
      await yieldTask();
      if (before === revision && messages === 0) quiet++; else quiet = 0;
      if (quiet === 2) return;
    }
    throw new Error('Virtual clock: React/message work did not settle');
  };
  function invoke(task) {
    if (task.idle) return task.callback({ didTimeout: false, timeRemaining: () => 50 });
    if (typeof task.callback === 'function') return task.callback(...task.args);
    return (0, eval)(String(task.callback));
  }
  function schedule(callback, delay, args, interval = false, idle = false) {
    delay = Math.max(interval || idle ? 1 : 0, Number(delay) || 0);
    const key = ++id;
    const task = { key, callback, args, delay, interval, idle, at: now + delay, handle: null };
    tasks.set(key, task);
    if (!active) {
      const fire = () => {
        if (!tasks.has(key)) return;
        if (!interval) tasks.delete(key);
        else task.handle = native.timeout(fire, delay);
        invoke(task);
      };
      task.handle = native.timeout(fire, delay);
    }
    return key;
  }
  window.setTimeout = (callback, delay = 0, ...args) => schedule(callback, delay, args);
  window.setInterval = (callback, delay = 0, ...args) => schedule(callback, delay, args, true);
  window.clearTimeout = window.clearInterval = key => {
    const task = tasks.get(key); if (task?.handle !== null) native.clear(task?.handle);
    tasks.delete(key);
  };
  window.requestIdleCallback = (callback, options) => schedule(callback, Math.min(options?.timeout ?? 1, 1), [], false, true);
  window.cancelIdleCallback = window.clearTimeout;
  window.requestAnimationFrame = callback => {
    const key = ++id;
    const task = { callback, handle: null };
    rafs.set(key, task);
    if (!active) task.handle = native.raf(t => { if (rafs.delete(key)) callback(t); });
    return key;
  };
  window.cancelAnimationFrame = key => { const task = rafs.get(key); if (task?.handle !== null) native.cancelRaf(task?.handle); rafs.delete(key); };
  function VirtualDate(...args) {
    if (!new.target) return new native.Date(active ? epoch + now : native.Date.now()).toString();
    return Reflect.construct(native.Date, args.length ? args : [active ? epoch + now : native.Date.now()], new.target);
  }
  Object.setPrototypeOf(VirtualDate, native.Date);
  VirtualDate.prototype = native.Date.prototype;
  VirtualDate.now = () => active ? epoch + now : native.Date.now();
  window.Date = VirtualDate;
  Object.defineProperty(performance, 'now', { value: () => active ? now : native.now() });
  const realOrigin = performance.timeOrigin;
  Object.defineProperty(performance, 'timeOrigin', { get: () => active ? epoch : realOrigin });
  function syncAnimations() {
    for (const animation of document.getAnimations()) {
      if (!animations.has(animation)) { animations.set(animation, now); animation.pause(); }
      animation.currentTime = Math.max(0, now - animations.get(animation));
    }
  }
  let seeking = false;
  window.__codeVideoClock = {
    async start(timestampMs = 0) {
      if (!Number.isFinite(timestampMs) || timestampMs < 0) throw new Error('Invalid virtual clock offset');
      if (active) throw new Error('Virtual clock already started');
      await settle();
      active = true;
      now = timestampMs;
      frame = Math.floor((now + 1e-7) / step) + 1;
      for (const task of tasks.values()) { native.clear(task.handle); task.at = now + task.delay; task.handle = null; }
      for (const task of rafs.values()) { native.cancelRaf(task.handle); task.handle = null; }
      syncAnimations();
      await settle();
    },
    settle,
    atBoundary(callback) {
      if (!active || typeof callback !== 'function') throw new Error('Boundary handshake requires an active clock');
      boundaries.push(callback);
    },
  };
  window.__virtualNow = () => now;
  window.__seek = async target => {
    if (!active || !Number.isFinite(target) || target < now - 1e-7 || seeking) throw new Error('Virtual clock requires sequential, forward seeks after start');
    seeking = true;
    let operations = 0;
    try {
      while (true) {
        if (++operations > 100000) throw new Error('Virtual clock callback limit exceeded');
        let next;
        for (const task of tasks.values()) {
          if (task.at <= target + 1e-7 && (!next || task.at < next.at || (task.at === next.at && task.key < next.key))) next = task;
        }
        // Finish all current-time effects before exporting a boundary. The
        // next action is released at the same time, before future timers/rAF.
        if (boundaries.length && (!next || next.at > now + 1e-7)) {
          await boundaries.shift()();
          await settle();
          syncAnimations();
          continue;
        }
        const frameAt = frame * step;
        if (next && next.at <= frameAt + 1e-7) {
          now = Math.max(now, next.at);
          if (next.interval) next.at += next.delay; else tasks.delete(next.key);
          invoke(next);
          await settle();
          syncAnimations();
          continue;
        }
        if (frameAt <= target + 1e-7) {
          now = frameAt; frame++;
          const callbacks = [...rafs.entries()];
          for (const [key, task] of callbacks) if (rafs.delete(key)) task.callback(now);
          await settle();
          syncAnimations();
          continue;
        }
        now = target;
        await settle();
        syncAnimations();
        // React effects may have queued a zero-delay timer while settling.
        if ([...tasks.values()].some(task => task.at <= target + 1e-7)) continue;
        return now;
      }
    } finally { seeking = false; }
  };
}
module.exports = { installVirtualClock };
