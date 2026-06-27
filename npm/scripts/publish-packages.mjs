import path from "node:path";
import { spawnSync } from "node:child_process";
import { npmRoot, packageJson, targets, version, wrapperDir } from "./package-metadata.mjs";

const tag = process.env.GITHUB_REF_NAME || "";
if (process.env.CI !== "true" || tag !== `v${version}`) {
  throw new Error(`Refusing to publish outside CI tag v${version}`);
}

function isPublished(name) {
  return spawnSync("npm", ["view", `${name}@${version}`, "version"], { stdio: "ignore" }).status === 0;
}

function publish(packageDir) {
  const manifest = packageJson(packageDir);
  if (isPublished(manifest.name)) {
    console.log(`${manifest.name}@${version} is already published; skipping.`);
    return;
  }
  const result = spawnSync("npm", ["publish", packageDir, "--access", "public", "--provenance"], {
    cwd: npmRoot,
    stdio: "inherit"
  });
  if (result.status !== 0) process.exit(result.status || 1);
}

for (const target of targets) publish(path.join(npmRoot, "packages", target.packageDir));
publish(wrapperDir);
