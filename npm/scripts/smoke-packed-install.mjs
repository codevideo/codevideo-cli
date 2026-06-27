import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { npmRoot, version } from "./package-metadata.mjs";

const temp = fs.mkdtempSync(path.join(os.tmpdir(), "codevideo-cli-pack-"));
const platform = `${process.platform}-${process.arch}`;
const outputDir = path.join(npmRoot, "dist", "npm");
const wrapperTarball = path.join(outputDir, `fullstackcraftllc-codevideo-cli-${version}.tgz`);
const platformTarball = path.join(outputDir, `fullstackcraftllc-codevideo-cli-${platform}-${version}.tgz`);

function run(command, args) {
  const result = spawnSync(command, args, {
    cwd: temp,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"]
  });
  if (result.status !== 0) throw new Error(`${command} ${args.join(" ")} failed:\n${result.stdout}${result.stderr}`);
  return result.stdout;
}

try {
  assert.ok(fs.existsSync(wrapperTarball), `Missing wrapper tarball: ${wrapperTarball}`);
  assert.ok(fs.existsSync(platformTarball), `Missing platform tarball: ${platformTarball}`);
  run("npm", ["init", "-y"]);
  run("npm", ["install", "--ignore-scripts", "--package-lock=false", wrapperTarball, platformTarball]);

  const scopeDir = path.join(temp, "node_modules", "@fullstackcraftllc");
  const platformPackages = fs.readdirSync(scopeDir).filter((name) => /^codevideo-cli-(darwin|linux|win32)-/.test(name));
  assert.deepEqual(platformPackages, [`codevideo-cli-${platform}`]);

  const executable = path.join(
    scopeDir,
    platformPackages[0],
    "bin",
    process.platform === "win32" ? "codevideo-cli.exe" : "codevideo-cli"
  );
  assert.match(run(executable, ["--version"]), new RegExp(`CodeVideo CLI v${version.replaceAll(".", "\\.")}`));
  console.log(`Packed wrapper selected exactly one native package: ${platformPackages[0]}.`);
} finally {
  fs.rmSync(temp, { recursive: true, force: true });
}
