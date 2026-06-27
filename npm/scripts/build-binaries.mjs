import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { npmRoot, repoRoot, targets, version } from "./package-metadata.mjs";

const releaseDir = path.join(npmRoot, "dist", "release");
fs.mkdirSync(releaseDir, { recursive: true });

const checksums = [];
for (const target of targets) {
  const binDir = path.join(npmRoot, "packages", target.packageDir, "bin");
  const output = path.join(binDir, target.executable);
  fs.mkdirSync(binDir, { recursive: true });
  const result = spawnSync("go", [
    "build",
    "-trimpath",
    "-ldflags", `-s -w -X main.version=${version}`,
    "-o", output,
    "."
  ], {
    cwd: repoRoot,
    env: {
      ...process.env,
      CGO_ENABLED: "0",
      GOOS: target.platform === "win32" ? "windows" : target.platform,
      GOARCH: target.goArch,
      GOTOOLCHAIN: "go1.26.4"
    },
    stdio: "inherit"
  });
  if (result.status !== 0) process.exit(result.status || 1);
  if (target.platform !== "win32") fs.chmodSync(output, 0o755);

  const releaseName = `codevideo-cli-${target.platform}-${target.arch}${target.platform === "win32" ? ".exe" : ""}`;
  const releasePath = path.join(releaseDir, releaseName);
  fs.copyFileSync(output, releasePath);
  if (target.platform !== "win32") fs.chmodSync(releasePath, 0o755);
  const digest = crypto.createHash("sha256").update(fs.readFileSync(releasePath)).digest("hex");
  checksums.push(`${digest}  ${releaseName}`);
}

fs.writeFileSync(path.join(releaseDir, "checksums.txt"), `${checksums.join("\n")}\n`);
