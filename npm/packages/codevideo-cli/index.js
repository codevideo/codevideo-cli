import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const packageRoot = path.dirname(fileURLToPath(import.meta.url));

const platformPackages = {
  "darwin-arm64": "@fullstackcraftllc/codevideo-cli-darwin-arm64",
  "darwin-x64": "@fullstackcraftllc/codevideo-cli-darwin-x64",
  "linux-arm64": "@fullstackcraftllc/codevideo-cli-linux-arm64",
  "linux-x64": "@fullstackcraftllc/codevideo-cli-linux-x64",
  "win32-arm64": "@fullstackcraftllc/codevideo-cli-win32-arm64",
  "win32-x64": "@fullstackcraftllc/codevideo-cli-win32-x64"
};

export function getCodeVideoCliPlatformPackage(platform = process.platform, arch = process.arch) {
  const packageName = platformPackages[`${platform}-${arch}`];
  if (!packageName) {
    throw new Error(`Unsupported CodeVideo CLI platform: ${platform}-${arch}`);
  }
  return packageName;
}

function validateExecutable(candidate, label, platform) {
  const stat = fs.statSync(candidate, { throwIfNoEntry: false });
  if (!stat?.isFile()) throw new Error(`${label} is not a file: ${candidate}`);
  if (platform !== "win32") {
    fs.accessSync(candidate, fs.constants.X_OK);
  }
}

export function resolveCodeVideoCliRuntime(options = {}) {
  const platform = options.platform || process.platform;
  const arch = options.arch || process.arch;
  const env = options.env || process.env;
  let binaryPath = options.binaryPath || env.PATH_TO_CODEVIDEO_CLI;

  if (binaryPath) {
    binaryPath = path.resolve(binaryPath);
  } else {
    const platformPackage = getCodeVideoCliPlatformPackage(platform, arch);
    let platformRoot;
    try {
      platformRoot = path.dirname(require.resolve(`${platformPackage}/package.json`));
    } catch (error) {
      throw new Error(
        `The optional package ${platformPackage} is missing. ` +
        "Reinstall @fullstackcraftllc/codevideo-cli without --omit=optional.",
        { cause: error }
      );
    }
    binaryPath = path.join(platformRoot, "bin", platform === "win32" ? "codevideo-cli.exe" : "codevideo-cli");
  }

  validateExecutable(binaryPath, "CodeVideo CLI", platform);

  const runnerPath = path.resolve(
    options.runnerPath || env.CODEVIDEO_PUPPETEER_RUNNER_PATH || path.join(packageRoot, "runtime", "recordVideoV3.js")
  );
  if (!fs.statSync(runnerPath, { throwIfNoEntry: false })?.isFile()) {
    throw new Error(`CodeVideo Puppeteer runner is missing: ${runnerPath}`);
  }

  const cacheRoot = path.join(os.homedir(), ".cache", "codevideo");
  const workDir = path.resolve(env.CODEVIDEO_WORK_DIR || path.join(os.tmpdir(), "codevideo", "v3"));
  const logDir = path.resolve(env.CODEVIDEO_LOG_DIR || path.join(cacheRoot, "logs"));
  const outputDir = path.resolve(env.CODEVIDEO_OUTPUT_DIR || process.cwd());

  return {
    binaryPath,
    runnerPath,
    workDir,
    logDir,
    outputDir,
    env: {
      ...env,
      CODEVIDEO_PUPPETEER_RUNNER_PATH: runnerPath,
      CODEVIDEO_WORK_DIR: workDir,
      CODEVIDEO_LOG_DIR: logDir,
      CODEVIDEO_OUTPUT_DIR: outputDir
    }
  };
}
