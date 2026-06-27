import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createRequire } from "node:module";
import {
  getCodeVideoCliPlatformPackage,
  resolveCodeVideoCliRuntime
} from "../packages/codevideo-cli/index.js";
import { runDoctor } from "../packages/codevideo-cli/doctor.js";
import { installBrowser } from "../packages/codevideo-cli/install-browser.js";

const require = createRequire(import.meta.url);
const runtimePaths = require("../../puppeteer-runner/runtimePaths.js");

test("maps every supported npm platform package", () => {
  const expected = {
    "darwin-arm64": "@fullstackcraftllc/codevideo-cli-darwin-arm64",
    "darwin-x64": "@fullstackcraftllc/codevideo-cli-darwin-x64",
    "linux-arm64": "@fullstackcraftllc/codevideo-cli-linux-arm64",
    "linux-x64": "@fullstackcraftllc/codevideo-cli-linux-x64",
    "win32-arm64": "@fullstackcraftllc/codevideo-cli-win32-arm64",
    "win32-x64": "@fullstackcraftllc/codevideo-cli-win32-x64"
  };
  for (const [key, packageName] of Object.entries(expected)) {
    const [platform, arch] = key.split("-");
    assert.equal(getCodeVideoCliPlatformPackage(platform, arch), packageName);
  }
  assert.throws(() => getCodeVideoCliPlatformPackage("freebsd", "x64"), /Unsupported/);
});

test("runtime resolver honors explicit binary and creates npm-safe paths", () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), "codevideo-cli-test-"));
  try {
    const binary = path.join(temp, "codevideo-cli");
    const runner = path.join(temp, "runner.js");
    fs.writeFileSync(binary, "#!/bin/sh\nexit 0\n");
    fs.writeFileSync(runner, "// runner\n");
    fs.chmodSync(binary, 0o755);
    const runtime = resolveCodeVideoCliRuntime({
      binaryPath: binary,
      runnerPath: runner,
      env: {
        CODEVIDEO_WORK_DIR: path.join(temp, "work"),
        CODEVIDEO_LOG_DIR: path.join(temp, "logs"),
        CODEVIDEO_OUTPUT_DIR: path.join(temp, "output")
      }
    });
    assert.equal(runtime.binaryPath, binary);
    assert.equal(runtime.env.CODEVIDEO_PUPPETEER_RUNNER_PATH, runner);
    assert.equal(runtime.env.CODEVIDEO_WORK_DIR, path.join(temp, "work"));
    assert.equal(runtime.env.CODEVIDEO_LOG_DIR, path.join(temp, "logs"));
    assert.equal(runtime.env.CODEVIDEO_OUTPUT_DIR, path.join(temp, "output"));
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
});

test("runtime resolver reports an omitted optional platform package", () => {
  assert.throws(
    () => resolveCodeVideoCliRuntime({ platform: "win32", arch: "x64", env: {} }),
    /optional package .*win32-x64.* is missing.*without --omit=optional/s
  );
});

test("doctor validates fixture binary, browser, ffmpeg, and directories", async () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), "codevideo-doctor-test-"));
  const originalChrome = process.env.CODEVIDEO_CHROME_PATH;
  const originalFfmpeg = process.env.CODEVIDEO_FFMPEG_PATH;
  try {
    const binary = path.join(temp, "codevideo-cli");
    const runner = path.join(temp, "runner.js");
    const chrome = path.join(temp, "chrome");
    const ffmpeg = path.join(temp, "ffmpeg");
    fs.writeFileSync(binary, "#!/bin/sh\necho '/> CodeVideo CLI v0.0.7'\n");
    fs.writeFileSync(runner, "// runner\n");
    fs.writeFileSync(chrome, "#!/bin/sh\nexit 0\n");
    fs.writeFileSync(ffmpeg, "#!/bin/sh\nexit 0\n");
    for (const executable of [binary, chrome, ffmpeg]) fs.chmodSync(executable, 0o755);
    process.env.CODEVIDEO_CHROME_PATH = chrome;
    process.env.CODEVIDEO_FFMPEG_PATH = ffmpeg;
    const status = await runDoctor({
      binaryPath: binary,
      runnerPath: runner,
      env: {
        ...process.env,
        CODEVIDEO_WORK_DIR: path.join(temp, "work"),
        CODEVIDEO_LOG_DIR: path.join(temp, "logs"),
        CODEVIDEO_OUTPUT_DIR: path.join(temp, "output")
      }
    });
    assert.equal(status, 0);
  } finally {
    if (originalChrome === undefined) delete process.env.CODEVIDEO_CHROME_PATH;
    else process.env.CODEVIDEO_CHROME_PATH = originalChrome;
    if (originalFfmpeg === undefined) delete process.env.CODEVIDEO_FFMPEG_PATH;
    else process.env.CODEVIDEO_FFMPEG_PATH = originalFfmpeg;
    fs.rmSync(temp, { recursive: true, force: true });
  }
});

test("runtime path helpers fail precisely when overrides are invalid", () => {
  const originalChrome = process.env.CODEVIDEO_CHROME_PATH;
  const originalFfmpeg = process.env.CODEVIDEO_FFMPEG_PATH;
  try {
    process.env.CODEVIDEO_CHROME_PATH = "/definitely/missing/chrome";
    process.env.CODEVIDEO_FFMPEG_PATH = "relative/ffmpeg";
    assert.throws(() => runtimePaths.resolveChromeExecutable(), /not an executable file/);
    assert.throws(() => runtimePaths.resolveFfmpegExecutable(), /not an absolute executable file/);
  } finally {
    if (originalChrome === undefined) delete process.env.CODEVIDEO_CHROME_PATH;
    else process.env.CODEVIDEO_CHROME_PATH = originalChrome;
    if (originalFfmpeg === undefined) delete process.env.CODEVIDEO_FFMPEG_PATH;
    else process.env.CODEVIDEO_FFMPEG_PATH = originalFfmpeg;
  }
});

test("doctor identifies missing Chrome and FFmpeg independently", async () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), "codevideo-doctor-failure-test-"));
  const originalError = console.error;
  const originalChrome = process.env.CODEVIDEO_CHROME_PATH;
  const originalFfmpeg = process.env.CODEVIDEO_FFMPEG_PATH;
  const messages = [];
  try {
    const binary = path.join(temp, "codevideo-cli");
    const runner = path.join(temp, "runner.js");
    const chrome = path.join(temp, "chrome");
    const ffmpeg = path.join(temp, "ffmpeg");
    fs.writeFileSync(binary, "#!/bin/sh\necho '/> CodeVideo CLI v0.0.7'\n");
    fs.writeFileSync(runner, "// runner\n");
    fs.writeFileSync(chrome, "#!/bin/sh\nexit 0\n");
    fs.writeFileSync(ffmpeg, "#!/bin/sh\nexit 0\n");
    for (const executable of [binary, chrome, ffmpeg]) fs.chmodSync(executable, 0o755);
    console.error = (message) => messages.push(String(message));

    process.env.CODEVIDEO_CHROME_PATH = path.join(temp, "missing-chrome");
    process.env.CODEVIDEO_FFMPEG_PATH = ffmpeg;
    assert.equal(await runDoctor({ binaryPath: binary, runnerPath: runner, env: process.env }), 1);
    assert.match(messages.pop(), /CODEVIDEO_CHROME_PATH.*not an executable file/);

    process.env.CODEVIDEO_CHROME_PATH = chrome;
    process.env.CODEVIDEO_FFMPEG_PATH = path.join(temp, "missing-ffmpeg");
    assert.equal(await runDoctor({ binaryPath: binary, runnerPath: runner, env: process.env }), 1);
    assert.match(messages.pop(), /CODEVIDEO_FFMPEG_PATH.*not an absolute executable file/);
  } finally {
    console.error = originalError;
    if (originalChrome === undefined) delete process.env.CODEVIDEO_CHROME_PATH;
    else process.env.CODEVIDEO_CHROME_PATH = originalChrome;
    if (originalFfmpeg === undefined) delete process.env.CODEVIDEO_FFMPEG_PATH;
    else process.env.CODEVIDEO_FFMPEG_PATH = originalFfmpeg;
    fs.rmSync(temp, { recursive: true, force: true });
  }
});

test("browser installation uses the configured cache without an install hook", async () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), "codevideo-browser-install-test-"));
  let installOptions;
  try {
    const executablePath = path.join(temp, "cache", "chrome", "fixture", "chrome");
    const result = await installBrowser({
      env: { CODEVIDEO_BROWSER_CACHE_DIR: path.join(temp, "cache") },
      browsers: {
        detectBrowserPlatform: () => "mac_arm",
        resolveBuildId: async () => "fixture-build",
        install: async (options) => {
          installOptions = options;
          return { executablePath };
        }
      }
    });
    assert.equal(result, executablePath);
    assert.equal(installOptions.cacheDir, path.join(temp, "cache"));
    assert.equal(installOptions.buildId, "fixture-build");

    const manifest = JSON.parse(fs.readFileSync(
      new URL("../packages/codevideo-cli/package.json", import.meta.url),
      "utf8"
    ));
    assert.equal(manifest.scripts?.install, undefined);
    assert.equal(manifest.scripts?.postinstall, undefined);
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
});
