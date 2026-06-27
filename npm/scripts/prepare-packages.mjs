import fs from "node:fs";
import path from "node:path";
import { npmRoot, repoRoot, targets, wrapperDir } from "./package-metadata.mjs";

fs.mkdirSync(path.join(wrapperDir, "runtime"), { recursive: true });
fs.copyFileSync(path.join(repoRoot, "puppeteer-runner", "recordVideoV3.js"), path.join(wrapperDir, "runtime", "recordVideoV3.js"));
fs.copyFileSync(path.join(repoRoot, "puppeteer-runner", "runtimePaths.js"), path.join(wrapperDir, "runtime", "runtimePaths.js"));
fs.chmodSync(path.join(wrapperDir, "bin", "codevideo-cli.js"), 0o755);

for (const packageDir of [wrapperDir, ...targets.map((target) => path.join(npmRoot, "packages", target.packageDir))]) {
  fs.copyFileSync(path.join(repoRoot, "LICENSE"), path.join(packageDir, "LICENSE"));
}
