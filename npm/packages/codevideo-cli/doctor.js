import fs from "node:fs";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import { resolveCodeVideoCliRuntime } from "./index.js";

const require = createRequire(import.meta.url);
const runtimePaths = require("./runtime/runtimePaths.js");

function checkWritableDirectory(directory) {
  fs.mkdirSync(directory, { recursive: true });
  fs.accessSync(directory, fs.constants.W_OK);
}

export async function runDoctor(options = {}) {
  const checks = [];
  try {
    const runtime = resolveCodeVideoCliRuntime(options);
    const version = spawnSync(runtime.binaryPath, ["--version"], { env: runtime.env, encoding: "utf8" });
    if (version.status !== 0) throw new Error(version.stderr || "native binary version check failed");
    checks.push(["native binary", runtime.binaryPath]);
    checks.push(["Puppeteer runner", runtime.runnerPath]);
    checks.push(["Chrome", runtimePaths.resolveChromeExecutable()]);
    checks.push(["FFmpeg", runtimePaths.resolveFfmpegExecutable()]);
    for (const directory of [runtime.workDir, runtime.logDir, runtime.outputDir]) {
      checkWritableDirectory(directory);
    }
    checks.push(["writable directories", `${runtime.workDir}, ${runtime.logDir}, ${runtime.outputDir}`]);
    for (const [name, value] of checks) console.log(`✓ ${name}: ${value}`);
    return 0;
  } catch (error) {
    for (const [name, value] of checks) console.log(`✓ ${name}: ${value}`);
    console.error(`✗ ${error.message}`);
    return 1;
  }
}
