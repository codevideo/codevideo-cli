import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { npmRoot, targets, wrapperDir } from "./package-metadata.mjs";

const outputDir = path.join(npmRoot, "dist", "npm");
fs.rmSync(outputDir, { recursive: true, force: true });
fs.mkdirSync(outputDir, { recursive: true });

const packageDirs = targets.map((target) => path.join(npmRoot, "packages", target.packageDir));
packageDirs.push(wrapperDir);
for (const packageDir of packageDirs) {
  const result = spawnSync("npm", ["pack", packageDir, "--pack-destination", outputDir], {
    cwd: npmRoot,
    stdio: "inherit"
  });
  if (result.status !== 0) process.exit(result.status || 1);
}
