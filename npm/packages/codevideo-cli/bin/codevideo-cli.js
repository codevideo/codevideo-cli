#!/usr/bin/env node

import { spawn } from "node:child_process";
import { runDoctor } from "../doctor.js";
import { installBrowser } from "../install-browser.js";
import { resolveCodeVideoCliRuntime } from "../index.js";

const args = process.argv.slice(2);

if (args[0] === "doctor") {
  process.exitCode = await runDoctor();
} else if (args[0] === "install-browser") {
  await installBrowser();
} else {
  const runtime = resolveCodeVideoCliRuntime();
  const child = spawn(runtime.binaryPath, args, {
    cwd: process.cwd(),
    env: runtime.env,
    stdio: "inherit"
  });

  for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"]) {
    process.on(signal, () => child.kill(signal));
  }
  child.once("error", (error) => {
    console.error(`Failed to start CodeVideo CLI: ${error.message}`);
    process.exitCode = 1;
  });
  child.once("exit", (code, signal) => {
    if (signal) process.kill(process.pid, signal);
    else process.exitCode = code ?? 1;
  });
}
