import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { npmRoot, packageJson, targets, version, wrapperDir } from "./package-metadata.mjs";

const wrapper = packageJson(wrapperDir);
if (wrapper.version !== version) throw new Error(`Wrapper version ${wrapper.version} does not match ${version}`);

function listFiles(directory, base = directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const absolute = path.join(directory, entry.name);
    return entry.isDirectory() ? listFiles(absolute, base) : [path.relative(base, absolute)];
  });
}

function expectedFileType(target) {
  if (target.platform === "darwin") return /Mach-O/;
  if (target.platform === "linux") return /ELF/;
  return /PE32\+/;
}

for (const target of targets) {
  const dir = path.join(npmRoot, "packages", target.packageDir);
  const manifest = packageJson(dir);
  const expectedName = `@fullstackcraftllc/${target.packageDir}`;
  if (manifest.name !== expectedName || manifest.version !== version) {
    throw new Error(`Invalid package metadata for ${target.packageDir}`);
  }
  if (wrapper.optionalDependencies[expectedName] !== version) {
    throw new Error(`Wrapper optional dependency for ${expectedName} is not pinned to ${version}`);
  }
  const binary = path.join(dir, "bin", target.executable);
  const stat = fs.statSync(binary, { throwIfNoEntry: false });
  if (!stat?.isFile()) throw new Error(`Missing binary: ${binary}`);
  if (stat.size < 15 * 1024 * 1024 || stat.size > 30 * 1024 * 1024) {
    throw new Error(`Binary size is outside the expected 15-30 MB range: ${binary}`);
  }
  if (target.platform !== "win32" && (stat.mode & 0o111) === 0) {
    throw new Error(`Binary is not executable: ${binary}`);
  }
  const fileType = spawnSync("file", ["-b", binary], { encoding: "utf8" });
  if (fileType.status !== 0 || !expectedFileType(target).test(fileType.stdout)) {
    throw new Error(`Unexpected file type for ${binary}: ${fileType.stdout}${fileType.stderr}`);
  }
  const contents = listFiles(dir).sort();
  const expectedContents = ["LICENSE", path.join("bin", target.executable), "package.json"].sort();
  if (JSON.stringify(contents) !== JSON.stringify(expectedContents)) {
    throw new Error(`Unexpected package contents for ${target.packageDir}: ${contents.join(", ")}`);
  }
}

for (const runtimeFile of ["recordVideoV3.js", "runtimePaths.js", "package.json"]) {
  if (!fs.statSync(path.join(wrapperDir, "runtime", runtimeFile), { throwIfNoEntry: false })?.isFile()) {
    throw new Error(`Missing wrapper runtime file: ${runtimeFile}`);
  }
}

const wrapperBytes = listFiles(wrapperDir).reduce(
  (total, relative) => total + fs.statSync(path.join(wrapperDir, relative)).size,
  0
);
if (wrapperBytes >= 1024 * 1024) throw new Error(`Wrapper package exceeds 1 MB: ${wrapperBytes} bytes`);

const current = targets.find((target) => target.platform === process.platform && target.arch === process.arch);
if (current) {
  const binary = path.join(npmRoot, "packages", current.packageDir, "bin", current.executable);
  const result = spawnSync(binary, ["--version"], { encoding: "utf8" });
  if (result.status !== 0 || !result.stdout.includes(`v${version}`)) {
    throw new Error(`Native version check failed: ${result.stdout}${result.stderr}`);
  }
}

console.log(`Verified wrapper and ${targets.length} platform packages at ${version}.`);
