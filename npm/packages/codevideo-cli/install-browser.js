import path from "node:path";
import { createRequire } from "node:module";
import {
  Browser,
  BrowserTag,
  detectBrowserPlatform,
  install,
  resolveBuildId
} from "@puppeteer/browsers";

const require = createRequire(import.meta.url);
const { browserCacheDir } = require("./runtime/runtimePaths.js");

export async function installBrowser(options = {}) {
  const browsers = options.browsers || {
    detectBrowserPlatform,
    install,
    resolveBuildId
  };
  const platform = browsers.detectBrowserPlatform();
  if (!platform) throw new Error(`Unsupported browser platform: ${process.platform}/${process.arch}`);
  const cacheDir = path.resolve(browserCacheDir(options.env || process.env));
  const buildId = await browsers.resolveBuildId(Browser.CHROME, platform, BrowserTag.STABLE);
  const installed = await browsers.install({
    browser: Browser.CHROME,
    buildId,
    buildIdAlias: BrowserTag.STABLE,
    cacheDir,
    platform,
    downloadProgressCallback: "default"
  });
  console.log(`Chrome for Testing installed at ${installed.executablePath}`);
  return installed.executablePath;
}
