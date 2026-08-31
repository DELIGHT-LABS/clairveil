import { build } from "esbuild";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const dappRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const temporaryDirectory = await mkdtemp(join(tmpdir(), "clairveil-dapp-bundle-"));
const generatedBundle = join(temporaryDirectory, "app.bundle.js");
const committedBundle = join(dappRoot, "public", "app.bundle.js");

try {
  await build({
    entryPoints: [join(dappRoot, "public", "app.js")],
    bundle: true,
    format: "esm",
    platform: "browser",
    target: "es2022",
    outfile: generatedBundle,
    logLevel: "silent",
  });
  const [expected, generated] = await Promise.all([
    readFile(committedBundle),
    readFile(generatedBundle),
  ]);
  if (!expected.equals(generated)) {
    throw new Error("public/app.bundle.js is stale; run npm run build:dapp and commit the updated bundle.");
  }
} finally {
  await rm(temporaryDirectory, { recursive: true, force: true });
}
