import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const npmRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
export const repoRoot = path.resolve(npmRoot, "..");
export const version = "0.0.7";

export const targets = [
  { platform: "darwin", arch: "arm64", goArch: "arm64", packageDir: "codevideo-cli-darwin-arm64", executable: "codevideo-cli" },
  { platform: "darwin", arch: "x64", goArch: "amd64", packageDir: "codevideo-cli-darwin-x64", executable: "codevideo-cli" },
  { platform: "linux", arch: "arm64", goArch: "arm64", packageDir: "codevideo-cli-linux-arm64", executable: "codevideo-cli" },
  { platform: "linux", arch: "x64", goArch: "amd64", packageDir: "codevideo-cli-linux-x64", executable: "codevideo-cli" },
  { platform: "win32", arch: "arm64", goArch: "arm64", packageDir: "codevideo-cli-win32-arm64", executable: "codevideo-cli.exe" },
  { platform: "win32", arch: "x64", goArch: "amd64", packageDir: "codevideo-cli-win32-x64", executable: "codevideo-cli.exe" }
];

export const wrapperDir = path.join(npmRoot, "packages", "codevideo-cli");

export function packageJson(packageDir) {
  return JSON.parse(fs.readFileSync(path.join(packageDir, "package.json"), "utf8"));
}
