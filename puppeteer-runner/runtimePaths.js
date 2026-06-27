const fs = require("fs");
const os = require("os");
const path = require("path");

function isExecutableFile(candidate) {
  if (!candidate) return false;
  try {
    const stat = fs.statSync(candidate);
    if (!stat.isFile()) return false;
    if (process.platform !== "win32") {
      fs.accessSync(candidate, fs.constants.X_OK);
    }
    return true;
  } catch {
    return false;
  }
}

function findOnPath(names) {
  const entries = (process.env.PATH || "").split(path.delimiter).filter(Boolean);
  const extensions = process.platform === "win32"
    ? (process.env.PATHEXT || ".EXE;.CMD;.BAT").split(";")
    : [""];
  for (const name of names) {
    for (const entry of entries) {
      for (const extension of extensions) {
        const candidate = path.join(entry, process.platform === "win32" ? `${name}${extension}` : name);
        if (isExecutableFile(candidate)) return candidate;
      }
    }
  }
  return null;
}

function chromeSubpath() {
  const macApp = "Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing";
  if (process.platform === "darwin") {
    return process.arch === "arm64" ? `chrome-mac-arm64/${macApp}` : `chrome-mac-x64/${macApp}`;
  }
  if (process.platform === "win32") return "chrome-win64/chrome.exe";
  return "chrome-linux64/chrome";
}

function findCachedChrome(chromeRoot) {
  if (!fs.existsSync(chromeRoot)) return null;
  const builds = fs.readdirSync(chromeRoot)
    .map((name) => path.join(chromeRoot, name))
    .filter((candidate) => {
      try { return fs.statSync(candidate).isDirectory(); } catch { return false; }
    })
    .sort()
    .reverse();
  for (const build of builds) {
    const candidate = path.join(build, chromeSubpath());
    if (isExecutableFile(candidate)) return candidate;
  }
  return null;
}

function browserCacheDir(env = process.env) {
  return env.CODEVIDEO_BROWSER_CACHE_DIR || path.join(os.homedir(), ".cache", "codevideo", "chrome");
}

function systemChromeCandidates() {
  if (process.platform === "darwin") {
    return [
      "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
      "/Applications/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing",
      path.join(os.homedir(), "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
    ];
  }
  if (process.platform === "win32") {
    return [
      process.env.PROGRAMFILES && path.join(process.env.PROGRAMFILES, "Google/Chrome/Application/chrome.exe"),
      process.env["PROGRAMFILES(X86)"] && path.join(process.env["PROGRAMFILES(X86)"], "Google/Chrome/Application/chrome.exe"),
      process.env.LOCALAPPDATA && path.join(process.env.LOCALAPPDATA, "Google/Chrome/Application/chrome.exe"),
    ].filter(Boolean);
  }
  return [];
}

function resolveChromeExecutable() {
  if (process.env.CODEVIDEO_CHROME_PATH) {
    if (!isExecutableFile(process.env.CODEVIDEO_CHROME_PATH)) {
      throw new Error(`CODEVIDEO_CHROME_PATH is not an executable file: ${process.env.CODEVIDEO_CHROME_PATH}`);
    }
    return process.env.CODEVIDEO_CHROME_PATH;
  }

  const managed = findCachedChrome(path.join(browserCacheDir(), "chrome"));
  if (managed) return managed;

  const legacy = findCachedChrome(path.join(__dirname, "chrome"));
  if (legacy) return legacy;

  for (const candidate of systemChromeCandidates()) {
    if (isExecutableFile(candidate)) return candidate;
  }

  const fromPath = findOnPath(["google-chrome-stable", "google-chrome", "chromium", "chromium-browser"]);
  if (fromPath) return fromPath;

  throw new Error(
    `Chrome was not found for ${process.platform}/${process.arch}. ` +
    "Set CODEVIDEO_CHROME_PATH or run `codevideo-cli install-browser`."
  );
}

function resolveFfmpegExecutable() {
  if (process.env.CODEVIDEO_FFMPEG_PATH) {
    if (!path.isAbsolute(process.env.CODEVIDEO_FFMPEG_PATH) || !isExecutableFile(process.env.CODEVIDEO_FFMPEG_PATH)) {
      throw new Error(`CODEVIDEO_FFMPEG_PATH is not an absolute executable file: ${process.env.CODEVIDEO_FFMPEG_PATH}`);
    }
    return process.env.CODEVIDEO_FFMPEG_PATH;
  }
  const executable = findOnPath(["ffmpeg"]);
  if (executable) return executable;
  throw new Error("FFmpeg was not found. Install ffmpeg on PATH or set CODEVIDEO_FFMPEG_PATH.");
}

module.exports = {
  browserCacheDir,
  isExecutableFile,
  resolveChromeExecutable,
  resolveFfmpegExecutable,
};
