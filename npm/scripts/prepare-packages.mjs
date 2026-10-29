import fs from "node:fs";
import path from "node:path";
import { npmRoot, repoRoot, targets, wrapperDir } from "./package-metadata.mjs";

fs.mkdirSync(path.join(wrapperDir, "runtime"), { recursive: true });
for (const file of ["recordVideoV3.js", "runtimePaths.js", "renderFramesV1.js", "virtualClock.js", "renderSegments.js"]) {
  fs.copyFileSync(path.join(repoRoot, "puppeteer-runner", file), path.join(wrapperDir, "runtime", file));
}
fs.chmodSync(path.join(wrapperDir, "bin", "codevideo-cli.js"), 0o755);

for (const packageDir of [wrapperDir, ...targets.map((target) => path.join(npmRoot, "packages", target.packageDir))]) {
  fs.copyFileSync(path.join(repoRoot, "LICENSE"), path.join(packageDir, "LICENSE"));
}
