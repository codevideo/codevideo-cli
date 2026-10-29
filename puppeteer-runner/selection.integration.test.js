const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const os = require('node:os');
const { pathToFileURL } = require('node:url');
const { createScene, render } = require('./renderFramesV1');

const lesson = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../codevideo-ide-react/scripts/golden/fixture-selection.json')));
const portraitProps = {
  theme: 'dark', isFileExplorerVisible: false, isEditorTabsVisible: false,
  isMinimapVisible: false, isTerminalVisible: false, wordWrap: 'on',
  keyboardTypingPauseMs: 40, standardPauseMs: 100,
};
const manifest = { uuid: 'selection-browser', renderSeed: 'selection-v1', lesson, codeVideoIDEProps: portraitProps };

async function serve() {
  const root = path.resolve(__dirname, '../cli/staticserver/public');
  const mime = { '.html': 'text/html', '.js': 'application/javascript', '.json': 'application/json', '.css': 'text/css', '.woff2': 'font/woff2', '.woff': 'font/woff', '.svg': 'image/svg+xml', '.png': 'image/png' };
  const server = http.createServer((req, res) => {
    let file = path.resolve(root, '.' + decodeURIComponent(new URL(req.url, 'http://localhost').pathname));
    if (!file.startsWith(root + path.sep)) { res.writeHead(403).end(); return; }
    if (fs.existsSync(file) && fs.statSync(file).isDirectory()) file = path.join(file, 'index.html');
    if (!fs.existsSync(file)) { res.writeHead(404).end(); return; }
    res.setHeader('Content-Type', mime[path.extname(file)] || 'application/octet-stream');
    fs.createReadStream(file).pipe(res);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  return { baseUrl: `http://127.0.0.1:${server.address().port}`, close: () => new Promise(resolve => server.close(resolve)) };
}

async function inspect(page) {
  return page.evaluate(() => {
    const api = window.monaco;
    const editor = api.editor.getEditors()[0], model = editor.getModel(), selection = editor.getSelection();
    return {
      index: window.__codeVideoRenderState.currentActionIndex,
      complete: window.__codeVideoRenderState.complete,
      error: window.__codeVideoRenderState.error,
      code: model?.getValue(), text: model && selection ? model.getValueInRange(selection) : '',
      anchor: selection && [selection.selectionStartLineNumber, selection.selectionStartColumn],
      active: selection && [selection.positionLineNumber, selection.positionColumn],
      tabs: !!document.querySelector('[data-testid="editor-tabs"]'),
      explorer: !!document.querySelector('[data-codevideo-id="file-explorer"]'),
      minimap: editor.getOption(api.editor.EditorOption.minimap).enabled,
      wrap: editor.getOption(api.editor.EditorOption.wordWrap),
      editorWidth: editor.getLayoutInfo().width,
      selectionPixels: [...document.querySelectorAll('.selected-text')].some(node => node.getBoundingClientRect().width > 0),
    };
  });
}

function assertSnapshot(actual, editor, label) {
  assert.equal(actual.code, editor.content, `${label}: content`);
  const highlight = editor.highlightCoordinates;
  const start = highlight?.start || editor.caretPosition;
  const end = highlight?.end || editor.caretPosition;
  assert.deepEqual(actual.anchor, [start.row, start.col], `${label}: anchor`);
  assert.deepEqual(actual.active, [end.row, end.col], `${label}: active endpoint`);
}

test('real Monaco selection, replay commits, layout options and checkpoint frames', {
  skip: process.env.CODEVIDEO_BROWSER_TESTS !== '1', timeout: 600000,
}, async () => {
  const host = await serve();
  const output = process.env.CODEVIDEO_SELECTION_TEST_OUTPUT || fs.mkdtempSync(path.join(os.tmpdir(), 'codevideo-selection-'));
  fs.mkdirSync(output, { recursive: true });
  const { VirtualIDE } = await import(pathToFileURL(path.resolve(__dirname, '../../codevideo-virtual-ide/dist/index.js')));
  const expected = lesson.actions.map((_, index) => {
    const ide = new VirtualIDE(lesson);
    ide.applyActions(lesson.actions.slice(0, index + 1));
    return ide.getCourseSnapshot().editorSnapshot.editors.find(editor => editor.isActive);
  });
  const browsers = new Set();
  try {
    for (const renderMode of ['realtime', 'deterministic']) {
      const scene = await createScene({ manifest, renderMode, useNativeClock: renderMode === 'realtime', resolution: '1080p', orientation: 'portrait', baseUrl: host.baseUrl, browsers });
      const { page } = scene;
      page.on('pageerror', error => console.error(`Browser failure (${renderMode}): ${error.stack}`));
      // Before starting replay, the built v3 page is in step mode. The initial
      // snapshot already contains a forward selection, so this also checks SSR hydration.
      if (renderMode === 'realtime') await new Promise(resolve => setTimeout(resolve, 50));
      else await page.evaluate(() => window.__seek(50));
      assertSnapshot(await inspect(page), expected[0], `${renderMode}: step mode`);
      await page.evaluate(() => window.__startRecording());
      const observed = new Map(), rightColumns = new Set();
      let selectionShot = false;
      for (let time = 50; time <= 12000; time += 10) {
        if (renderMode === 'realtime') {
          await new Promise(resolve => setTimeout(resolve, 10));
          await page.evaluate(() => window.__flushCodeVideoFrame());
        } else await page.evaluate(async time => { await window.__seek(time); window.__flushCodeVideoFrame(); }, time);
        const actual = await inspect(page);
        assert.equal(actual.error, null);
        assert.equal(scene.failures.length, 0, `at ${renderMode} action ${actual.index}, time ${time}: ${scene.failures.join('\n')}`);
        if (actual.index === 2 && actual.active) rightColumns.add(actual.active[1]);
        if (lesson.actions[actual.index]?.name === 'author-wait') {
          const firstSeen = observed.get(actual.index);
          if (firstSeen === undefined) observed.set(actual.index, time);
          // Allow the documented 0ms/10ms state/model timers to settle.
          if (firstSeen !== undefined && time - firstSeen >= 30) {
            assertSnapshot(actual, expected[actual.index], `${renderMode}: action ${actual.index}`);
            if (actual.index === 20 && !selectionShot) {
              assert.equal(actual.selectionPixels, true, 'selection must be visibly painted');
              await page.screenshot({ path: path.join(output, `selection-${renderMode}.png`) });
              selectionShot = true;
              assert.equal(actual.tabs, false); assert.equal(actual.explorer, false);
              assert.equal(actual.minimap, false); assert.equal(actual.wrap, 'on');
              assert.ok(actual.editorWidth > 1000, 'hidden chrome must reclaim portrait width');
            }
          }
        }
        if (actual.complete) { assertSnapshot(actual, expected.at(-1), `${renderMode}: complete`); break; }
        assert.ok(time < 12000, 'replay timed out');
      }
      assert.ok(selectionShot);
      for (const column of [2, 3, 4, 5]) assert.ok(rightColumns.has(column), `missing animated selection column ${column}`);
      assert.equal(observed.size, lesson.actions.filter(action => action.name === 'author-wait').length);
      await scene.browser.close(); browsers.delete(scene.browser);
    }
    // Explicit true/off values must also reach Monaco through the same manifest path.
    const visible = await createScene({ manifest: { ...manifest, codeVideoIDEProps: { ...portraitProps, isFileExplorerVisible: true, isEditorTabsVisible: true, isMinimapVisible: true, wordWrap: 'off' } }, resolution: '1080p', orientation: 'portrait', baseUrl: host.baseUrl, browsers });
    await visible.page.evaluate(() => window.__seek(50));
    const options = await inspect(visible.page);
    assert.equal(options.tabs, true); assert.equal(options.explorer, true); assert.equal(options.minimap, true); assert.equal(options.wrap, 'off');
    await visible.browser.close(); browsers.delete(visible.browser);

    if (process.env.CODEVIDEO_SELECTION_RENDER === '1') {
      const results = [];
      for (const [strategy, workers] of [['sequential-seek', 1], ['ab-initio', 2]]) {
        const stem = path.join(output, `portrait-${strategy}`);
        results.push(await render({ manifest, strategy, workers, resolution: '1080p', orientation: 'portrait', baseUrl: host.baseUrl,
          output: stem + '.mp4', trace: stem + '.trace.json', metrics: stem + '.metrics.json', hashAllFrames: true }));
      }
      assert.deepEqual(results[1].trace, results[0].trace);
      assert.ok(results[1].metrics.eligibleCheckpoints > 0);
      assert.deepEqual(results[1].metrics.sampledFrames, results[0].metrics.sampledFrames, 'restored portrait frames must match uninterrupted replay');
    }
    console.log(`Selection screenshots and portrait renders: ${output}`);
  } finally {
    await Promise.all([...browsers].map(browser => browser.close()));
    await host.close();
  }
});
