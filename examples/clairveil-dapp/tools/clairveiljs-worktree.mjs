import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import {
  lstat,
  readFile,
  realpath,
  symlink,
  unlink
} from "node:fs/promises";
import { dirname, isAbsolute, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const toolDirectory = dirname(fileURLToPath(import.meta.url));
export const defaultDappDirectory = resolve(toolDirectory, "..");
const clairveilBundleVersion = "v0.3.1";
const clairveilSourceRepository = "https://github.com/DELIGHT-LABS/clairveil";
const clairveilSourceKind = "commit_snapshot";
const clairveilSourceCommit = "0ff92839872de26b787a60d8e4d5822cc459855b";
const clairveilReleaseContractFileCount = 17;

function sha256(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

function pathInside(root, relativePath, label) {
  const normalized = String(relativePath || "").trim();
  if (!normalized || isAbsolute(normalized)) {
    throw new Error(`${label} must be a non-empty relative path`);
  }
  const target = resolve(root, normalized);
  const relativeTarget = relative(resolve(root), target);
  if (!relativeTarget || relativeTarget === ".." || relativeTarget.startsWith(`..${sep}`) || isAbsolute(relativeTarget)) {
    throw new Error(`${label} escapes its contract root: ${normalized}`);
  }
  return target;
}

async function sha256File(path, label) {
  try {
    return sha256(await readFile(path));
  } catch (error) {
    throw new Error(`cannot read ${label}: ${path}`, { cause: error });
  }
}

function withoutConformanceFixtureOverride(environment) {
  const childEnvironment = { ...environment };
  delete childEnvironment.CLAIRVEIL_CONFORMANCE_FIXTURE_DIR;
  return childEnvironment;
}

export function resolveClairveilJSDirectory({
  environment = process.env,
  dappDirectory = defaultDappDirectory
} = {}) {
  const configured = String(environment.CLAIRVEILJS_DIR || "").trim();
  if (!configured) return resolve(dappDirectory, "../../../clairveiljs");
  return isAbsolute(configured) ? resolve(configured) : resolve(dappDirectory, configured);
}

async function validatedClairveilJSDirectory(options = {}) {
  const sdkDirectory = resolveClairveilJSDirectory(options);
  let packageMetadata;
  try {
    packageMetadata = JSON.parse(await readFile(resolve(sdkDirectory, "package.json"), "utf8"));
  } catch (error) {
    throw new Error(`CLAIRVEILJS_DIR does not contain a readable package.json: ${sdkDirectory}`, {
      cause: error
    });
  }
  if (packageMetadata?.name !== "clairveiljs" || packageMetadata?.version !== "0.3.1") {
    throw new Error(
      `CLAIRVEILJS_DIR must contain clairveiljs v0.3.1, got ${packageMetadata?.name || "unknown"}@${packageMetadata?.version || "unknown"}`
    );
  }
  return realpath(sdkDirectory);
}

export async function verifyClairveilJSContractSync({
  environment = process.env,
  dappDirectory = defaultDappDirectory,
  coreDirectory = resolve(dappDirectory, "../..")
} = {}) {
  const sdkDirectory = await validatedClairveilJSDirectory({ environment, dappDirectory });
  const resolvedCoreDirectory = await realpath(coreDirectory);
  const manifestPath = resolve(
    sdkDirectory,
    `fixtures/clairveil-${clairveilBundleVersion}/manifest.json`
  );
  let manifest;
  try {
    manifest = JSON.parse(await readFile(manifestPath, "utf8"));
  } catch (error) {
    throw new Error(`cannot read ClairveilJS release manifest: ${manifestPath}`, {
      cause: error
    });
  }
  if (manifest?.manifest_version !== 2) {
    throw new Error(`ClairveilJS contract manifest_version must be 2, got ${manifest?.manifest_version}`);
  }
  const sourceKeys = Object.keys(manifest?.source || {}).sort();
  if (
    sourceKeys.length !== 3 ||
    sourceKeys.some((key, index) => key !== ["commit", "kind", "repository"][index])
  ) {
    throw new Error("ClairveilJS contract manifest source must identify only an exact commit snapshot");
  }
  if (
    manifest.source.repository !== clairveilSourceRepository ||
    manifest.source.kind !== clairveilSourceKind ||
    manifest.source.commit !== clairveilSourceCommit
  ) {
    throw new Error(
      `ClairveilJS contract manifest must target ${clairveilSourceKind} ${clairveilSourceCommit}`
    );
  }
  if (!Array.isArray(manifest.files) || manifest.files.length !== clairveilReleaseContractFileCount) {
    throw new Error(
      `ClairveilJS release manifest must contain ${clairveilReleaseContractFileCount} contract files, got ${manifest?.files?.length ?? "invalid"}`
    );
  }

  const localPaths = new Set();
  const upstreamPaths = new Set();
  for (const [index, entry] of manifest.files.entries()) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
      throw new Error(`ClairveilJS release manifest file ${index} must be an object`);
    }
    const localPath = String(entry.local_path || "").trim();
    const upstreamPath = String(entry.upstream_path || "").trim();
    if (localPaths.has(localPath) || upstreamPaths.has(upstreamPath)) {
      throw new Error(`ClairveilJS release manifest contains a duplicate contract path at file ${index}`);
    }
    localPaths.add(localPath);
    upstreamPaths.add(upstreamPath);
    if (!/^[0-9a-f]{64}$/.test(String(entry.sha256 || ""))) {
      throw new Error(`ClairveilJS release manifest has an invalid SHA-256 for ${localPath || index}`);
    }

    const sdkPath = pathInside(sdkDirectory, localPath, `manifest files[${index}].local_path`);
    const corePath = pathInside(resolvedCoreDirectory, upstreamPath, `manifest files[${index}].upstream_path`);
    const [sdkHash, coreHash] = await Promise.all([
      sha256File(sdkPath, `bundled ClairveilJS contract ${localPath}`),
      sha256File(corePath, `current Clairveil core contract ${upstreamPath}`)
    ]);
    if (sdkHash !== entry.sha256) {
      throw new Error(
        `bundled ClairveilJS contract does not match its manifest: ${localPath}; expected ${entry.sha256}, got ${sdkHash}`
      );
    }
    if (coreHash !== sdkHash) {
      throw new Error(
        `ClairveilJS/core release contract drift: ${localPath} != ${upstreamPath}; SDK ${sdkHash}, core ${coreHash}`
      );
    }
  }

  return Object.freeze({
    sdkDirectory,
    coreDirectory: resolvedCoreDirectory,
    manifestPath,
    fileCount: manifest.files.length
  });
}

export async function linkConfiguredClairveilJS({
  environment = process.env,
  dappDirectory = defaultDappDirectory
} = {}) {
  const sdkDirectory = await validatedClairveilJSDirectory({ environment, dappDirectory });
  const dependencyPath = resolve(dappDirectory, "node_modules/clairveiljs");
  let dependencyStat;
  try {
    dependencyStat = await lstat(dependencyPath);
  } catch (error) {
    if (error?.code !== "ENOENT") throw error;
  }

  if (dependencyStat && !dependencyStat.isSymbolicLink()) {
    throw new Error(
      `${dependencyPath} is not a symbolic link; refusing to replace it. Run npm ci before selecting CLAIRVEILJS_DIR.`
    );
  }
  if (dependencyStat) {
    let linkedDirectory = "";
    try {
      linkedDirectory = await realpath(dependencyPath);
    } catch {
      // A broken SDK symlink is safe to replace after lstat proved that only the
      // link itself, rather than a dependency directory, will be removed.
    }
    if (linkedDirectory === sdkDirectory) return sdkDirectory;
    await unlink(dependencyPath);
  }

  // node_modules is generated state, so an absolute link is preferable here:
  // it also avoids macOS /var -> /private/var path aliasing producing a broken
  // relative target. No absolute path is written to source or the lockfile.
  await symlink(sdkDirectory, dependencyPath, "dir");
  const linkedDirectory = await realpath(dependencyPath);
  if (linkedDirectory !== sdkDirectory) {
    throw new Error(`failed to link ClairveilJS worktree: expected ${sdkDirectory}, got ${linkedDirectory}`);
  }
  return sdkDirectory;
}

export async function runClairveilJSScript(script, args = [], {
  environment = process.env,
  dappDirectory = defaultDappDirectory
} = {}) {
  if (!script) throw new Error("ClairveilJS npm script name is required");
  const sdkDirectory = await validatedClairveilJSDirectory({ environment, dappDirectory });
  const childEnvironment = clairveilJSScriptEnvironment(script, {
    environment,
    dappDirectory
  });
  const npmCommand = process.platform === "win32" ? "npm.cmd" : "npm";
  const child = spawn(npmCommand, ["run", script, ...args], {
    cwd: sdkDirectory,
    stdio: "inherit",
    env: childEnvironment
  });
  return new Promise((resolveExit, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      if (signal) {
        reject(new Error(`ClairveilJS npm script ${script} terminated by ${signal}`));
        return;
      }
      resolveExit(code ?? 1);
    });
  });
}

export function clairveilJSScriptEnvironment(script, {
  environment = process.env
} = {}) {
  const usesBundledReleaseFixtures = script === "test:conformance:required" ||
    script === "verify:release" ||
    script === "verify:release:integration" ||
    script === "prepublishOnly";
  return usesBundledReleaseFixtures
    ? withoutConformanceFixtureOverride(environment)
    : { ...environment };
}

async function main() {
  const [command, script, ...args] = process.argv.slice(2);
  if (command === "link") {
    const linked = await linkConfiguredClairveilJS();
    process.stdout.write(`ClairveilJS worktree: ${linked}\n`);
    return;
  }
  if (command === "verify-contract-sync") {
    const result = await verifyClairveilJSContractSync();
    process.stdout.write(
      `Verified ${result.fileCount} ClairveilJS bundled contracts against current Clairveil core.\n`
    );
    return;
  }
  if (command === "run") {
    process.exitCode = await runClairveilJSScript(script, args);
    return;
  }
  throw new Error("usage: clairveiljs-worktree.mjs <link|verify-contract-sync|run SCRIPT [ARGS...]");
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch(error => {
    process.stderr.write(`${error?.stack || error}\n`);
    process.exitCode = 1;
  });
}
