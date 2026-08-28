import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";
import { mkdtemp, mkdir, readFile, realpath, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, relative, resolve } from "node:path";

import {
  clairveilJSScriptEnvironment,
  linkConfiguredClairveilJS,
  resolveClairveilJSDirectory,
  verifyClairveilJSContractSync
} from "../tools/clairveiljs-worktree.mjs";

const releaseContractFileCount = 17;

function sha256(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

async function fakeSdk(directory, version = "0.3.1") {
  await mkdir(directory, { recursive: true });
  await writeFile(resolve(directory, "package.json"), JSON.stringify({
    name: "clairveiljs",
    version
  }));
}

async function fakeReleaseContracts(sdkDirectory, coreDirectory) {
  const files = [];
  for (let index = 0; index < releaseContractFileCount; index += 1) {
    const localPath = `fixtures/contracts/contract-${index}.json`;
    const upstreamPath = `contracts/contract-${index}.json`;
    const content = Buffer.from(JSON.stringify({ index, contract: `contract-${index}` }));
    await mkdir(dirname(resolve(sdkDirectory, localPath)), { recursive: true });
    await mkdir(dirname(resolve(coreDirectory, upstreamPath)), { recursive: true });
    await writeFile(resolve(sdkDirectory, localPath), content);
    await writeFile(resolve(coreDirectory, upstreamPath), content);
    files.push({
      kind: "conformance_fixture",
      local_path: localPath,
      upstream_path: upstreamPath,
      sha256: sha256(content)
    });
  }
  const manifestPath = resolve(
    sdkDirectory,
    "fixtures/clairveil-v0.3.1/manifest.json"
  );
  await mkdir(dirname(manifestPath), { recursive: true });
  await writeFile(manifestPath, JSON.stringify({
    manifest_version: 2,
    source: {
      repository: "https://github.com/DELIGHT-LABS/clairveil",
      kind: "commit_snapshot",
      commit: "0ff92839872de26b787a60d8e4d5822cc459855b"
    },
    files
  }));
  return { files, manifestPath };
}

test("ClairveilJS worktree selection is environment-driven and replaces symlinks only", async () => {
  const root = await mkdtemp(resolve(tmpdir(), "clairveil-web-sdk-link-"));
  try {
    const dappDirectory = resolve(root, "repo/examples/clairveil-dapp");
    const defaultSdk = resolve(root, "clairveiljs");
    const selectedSdk = resolve(root, "clairveiljs-evm-v0.3.1");
    const dependencyPath = resolve(dappDirectory, "node_modules/clairveiljs");
    await fakeSdk(defaultSdk);
    await fakeSdk(selectedSdk);
    await mkdir(dirname(dependencyPath), { recursive: true });
    await symlink(relative(dirname(dependencyPath), defaultSdk), dependencyPath, "dir");

    const environment = { CLAIRVEILJS_DIR: selectedSdk };
    assert.equal(
      resolveClairveilJSDirectory({ environment, dappDirectory }),
      selectedSdk
    );
    assert.equal(
      await linkConfiguredClairveilJS({ environment, dappDirectory }),
      await realpath(selectedSdk)
    );
    assert.equal(await realpath(dependencyPath), await realpath(selectedSdk));
    assert.equal(JSON.parse(await readFile(resolve(dependencyPath, "package.json"))).version, "0.3.1");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("ClairveilJS worktree selection refuses a non-symlink dependency directory", async () => {
  const root = await mkdtemp(resolve(tmpdir(), "clairveil-web-sdk-link-"));
  try {
    const dappDirectory = resolve(root, "repo/examples/clairveil-dapp");
    const selectedSdk = resolve(root, "clairveiljs-evm-v0.3.1");
    await fakeSdk(selectedSdk);
    await mkdir(resolve(dappDirectory, "node_modules/clairveiljs"), { recursive: true });

    await assert.rejects(
      () => linkConfiguredClairveilJS({
        environment: { CLAIRVEILJS_DIR: selectedSdk },
        dappDirectory
      }),
      /not a symbolic link; refusing to replace/
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("required conformance and SDK release scripts remove fixture overrides", () => {
  const dappDirectory = "/tmp/clairveil-repo/examples/clairveil-dapp";
  const required = clairveilJSScriptEnvironment("test:conformance:required", {
    environment: {
      KEEP: "yes",
      CLAIRVEIL_CONFORMANCE_FIXTURE_DIR: "/explicit"
    },
    dappDirectory
  });
  assert.deepEqual(required, { KEEP: "yes" });

  const development = clairveilJSScriptEnvironment("test:conformance", {
    environment: { KEEP: "yes" },
    dappDirectory
  });
  assert.deepEqual(development, { KEEP: "yes" });
  assert.deepEqual(
    clairveilJSScriptEnvironment("test:conformance", {
      environment: {
        KEEP: "yes",
        CLAIRVEIL_CONFORMANCE_FIXTURE_DIR: "/diagnostic-fixtures"
      },
      dappDirectory
    }),
    {
      KEEP: "yes",
      CLAIRVEIL_CONFORMANCE_FIXTURE_DIR: "/diagnostic-fixtures"
    }
  );

  for (const script of ["verify:release", "verify:release:integration", "prepublishOnly"]) {
    assert.deepEqual(
      clairveilJSScriptEnvironment(script, {
        environment: {
          KEEP: "yes",
          CLAIRVEIL_CONFORMANCE_FIXTURE_DIR: "/explicit"
        },
        dappDirectory
      }),
      { KEEP: "yes" },
      `${script} must use the SDK's bundled release fixtures`
    );
  }
});

test("release contract sync verifies all 17 SDK manifest files against current core", async () => {
  const root = await mkdtemp(resolve(tmpdir(), "clairveil-web-contract-sync-"));
  try {
    const coreDirectory = resolve(root, "repo");
    const dappDirectory = resolve(coreDirectory, "examples/clairveil-dapp");
    const sdkDirectory = resolve(root, "clairveiljs");
    await fakeSdk(sdkDirectory);
    await fakeReleaseContracts(sdkDirectory, coreDirectory);

    const result = await verifyClairveilJSContractSync({
      environment: { CLAIRVEILJS_DIR: sdkDirectory },
      dappDirectory
    });
    assert.equal(result.fileCount, releaseContractFileCount);
    assert.equal(result.sdkDirectory, await realpath(sdkDirectory));
    assert.equal(result.coreDirectory, await realpath(coreDirectory));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("release contract sync rejects a current core contract mutation", async () => {
  const root = await mkdtemp(resolve(tmpdir(), "clairveil-web-contract-sync-"));
  try {
    const coreDirectory = resolve(root, "repo");
    const dappDirectory = resolve(coreDirectory, "examples/clairveil-dapp");
    const sdkDirectory = resolve(root, "clairveiljs");
    await fakeSdk(sdkDirectory);
    const { files } = await fakeReleaseContracts(sdkDirectory, coreDirectory);
    await writeFile(
      resolve(coreDirectory, files[8].upstream_path),
      JSON.stringify({ mutated: true })
    );

    await assert.rejects(
      () => verifyClairveilJSContractSync({
        environment: { CLAIRVEILJS_DIR: sdkDirectory },
        dappDirectory
      }),
      /ClairveilJS\/core release contract drift:.*contract-8\.json/
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("release contract sync rejects a tag label on the commit snapshot", async () => {
  const root = await mkdtemp(resolve(tmpdir(), "clairveil-web-contract-source-"));
  try {
    const coreDirectory = resolve(root, "repo");
    const dappDirectory = resolve(coreDirectory, "examples/clairveil-dapp");
    const sdkDirectory = resolve(root, "clairveiljs");
    await fakeSdk(sdkDirectory);
    const { manifestPath } = await fakeReleaseContracts(sdkDirectory, coreDirectory);
    const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
    manifest.source.release = "v0.3.1";
    await writeFile(manifestPath, JSON.stringify(manifest));

    await assert.rejects(
      () => verifyClairveilJSContractSync({
        environment: { CLAIRVEILJS_DIR: sdkDirectory },
        dappDirectory
      }),
      /source must identify only an exact commit snapshot/
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("release contract sync rejects the obsolete manifest v1 contract", async () => {
  const root = await mkdtemp(resolve(tmpdir(), "clairveil-web-contract-version-"));
  try {
    const coreDirectory = resolve(root, "repo");
    const dappDirectory = resolve(coreDirectory, "examples/clairveil-dapp");
    const sdkDirectory = resolve(root, "clairveiljs");
    await fakeSdk(sdkDirectory);
    const { manifestPath } = await fakeReleaseContracts(sdkDirectory, coreDirectory);
    const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
    manifest.manifest_version = 1;
    await writeFile(manifestPath, JSON.stringify(manifest));

    await assert.rejects(
      () => verifyClairveilJSContractSync({
        environment: { CLAIRVEILJS_DIR: sdkDirectory },
        dappDirectory
      }),
      /manifest_version must be 2/
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("release contract sync rejects a different source commit", async () => {
  const root = await mkdtemp(resolve(tmpdir(), "clairveil-web-contract-commit-"));
  try {
    const coreDirectory = resolve(root, "repo");
    const dappDirectory = resolve(coreDirectory, "examples/clairveil-dapp");
    const sdkDirectory = resolve(root, "clairveiljs");
    await fakeSdk(sdkDirectory);
    const { manifestPath } = await fakeReleaseContracts(sdkDirectory, coreDirectory);
    const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
    manifest.source.commit = "0000000000000000000000000000000000000000";
    await writeFile(manifestPath, JSON.stringify(manifest));

    await assert.rejects(
      () => verifyClairveilJSContractSync({
        environment: { CLAIRVEILJS_DIR: sdkDirectory },
        dappDirectory
      }),
      /must target commit_snapshot/
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("production deployment verification links the configured SDK before importing it", async () => {
  const packageMetadata = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  assert.equal(
    packageMetadata.scripts["test:release-contracts"],
    "npm run verify:clairveiljs-contract-sync && node tools/clairveiljs-worktree.mjs run verify:clairveil-source && node tools/clairveiljs-worktree.mjs run verify:evm-source && node tools/clairveiljs-worktree.mjs run test:conformance:required"
  );
  assert.match(
    packageMetadata.scripts["verify:production-deployment"],
    /^npm run link:clairveiljs && node tools\/verify-production-deployment\.mjs$/
  );
});

test("core CI requires one immutable ClairveilJS commit instead of a mutable branch fallback", async () => {
  const workflow = await readFile(
    new URL("../../../.github/workflows/test.yml", import.meta.url),
    "utf8"
  );
  assert.match(workflow, /CLAIRVEILJS_REF: \$\{\{ vars\.CLAIRVEILJS_REF \}\}/);
  assert.match(workflow, /\^\[0-9a-f\]\{40\}\$/);
  assert.match(workflow, /ref: \$\{\{ steps\.clairveiljs-ref\.outputs\.sha \}\}/);
  assert.match(workflow, /actual_ref=.*rev-parse HEAD[\s\S]*actual_ref.*CLAIRVEILJS_REF/);
  assert.doesNotMatch(workflow, /CLAIRVEILJS_REF[^\n]*\|\|[^\n]*main/);
  assert.match(workflow, /repository: Hashed-Open-Finance\/maroo/);
  assert.match(workflow, /ref: d624bb76cbd8c4cc0a88d30c2a720aab6da28f75/);
  assert.match(workflow, /CLAIRVEIL_EVM_SOURCE_DIR: \$\{\{ github\.workspace \}\}\/\.ci\/maroo/);
});
