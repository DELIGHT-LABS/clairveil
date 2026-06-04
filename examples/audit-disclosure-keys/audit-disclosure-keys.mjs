import { createHash, randomBytes } from "node:crypto";
import { pathToFileURL } from "node:url";

const ROOT_SIGNING_DOMAIN = "clairveil-root-v1";
const DISCLOSURE_DOMAIN = "privacy-disclosure";

const Q =
  21888242871839275222246405745257275088548364400416034343698204186575808495617n;
const ORDER =
  2736030358979909402780800718157159386076813972158567259200215660948447373041n;
const D =
  12181644023421730124874158521699555681764249180949974110617291017600649128846n;

const BASE = {
  x: 9671717474070082183213120605117400219616337014328744928644933853176787189663n,
  y: 16950150798460657717958625567821834550301663161624707787222815936182638968203n,
};

const mod = (x) => ((x % Q) + Q) % Q;

const pow = (a, e) => {
  let result = 1n;
  let base = mod(a);

  while (e > 0n) {
    if (e & 1n) result = mod(result * base);
    base = mod(base * base);
    e >>= 1n;
  }

  return result;
};

const inv = (a) => pow(a, Q - 2n);

const add = (p, q) => {
  const xyxy = mod(p.x * q.x * p.y * q.y);

  return {
    x: mod((p.x * q.y + p.y * q.x) * inv(1n + D * xyxy)),
    y: mod((p.y * q.y + p.x * q.x) * inv(1n - D * xyxy)),
  };
};

const scalarMul = (point, scalar) => {
  let acc = { x: 0n, y: 1n };
  let cur = point;

  for (let n = scalar; n > 0n; n >>= 1n) {
    if (n & 1n) acc = add(acc, cur);
    cur = add(cur, cur);
  }

  return acc;
};

const toFixedHex = (n) => n.toString(16).padStart(64, "0");

const normalizeHex = (value) => {
  const hex = value.trim().replace(/^0x/i, "");
  if (!/^[0-9a-fA-F]+$/.test(hex)) {
    throw new Error("value must be hex");
  }
  return hex;
};

const hexFromBytesLike = (value, label) => {
  if (typeof value === "string") {
    return normalizeHex(value).toLowerCase();
  }
  if (value instanceof Uint8Array) {
    return Buffer.from(value).toString("hex");
  }
  throw new Error(`${label} must be hex or Uint8Array`);
};

const pickMaterialHex = (material, hexKey, bytesKey, label) => {
  if (material[hexKey] !== undefined) {
    return hexFromBytesLike(material[hexKey], label);
  }
  if (material[bytesKey] !== undefined) {
    return hexFromBytesLike(material[bytesKey], label);
  }
  throw new Error(`${label} is required`);
};

// Matches gnark-crypto bn254/twistededwards PointAffine.Bytes().
const compressPointHex = (point) => {
  const out = new Uint8Array(32);
  let y = point.y;

  for (let i = 0; i < 32; i++) {
    out[i] = Number(y & 0xffn);
    y >>= 8n;
  }

  if (point.x > (Q - 1n) / 2n) {
    out[31] |= 0x80;
  }

  return Buffer.from(out).toString("hex");
};

const keypairFromScalar = (scalar) => {
  if (scalar <= 0n || scalar >= ORDER) {
    throw new Error("private scalar must satisfy 1 <= scalar < ORDER");
  }

  const privateKeyHex = toFixedHex(scalar);
  const publicKeyHex = compressPointHex(scalarMul(BASE, scalar));

  return {
    privateKeyHex,
    publicKeyHex,
    publicKeyGenesisBase64: Buffer.from(publicKeyHex, "hex").toString("base64"),
  };
};

export const keypairFromPrivateKeyHex = (privateKeyHex) => {
  return keypairFromScalar(BigInt(`0x${normalizeHex(privateKeyHex)}`));
};

export const keypairFromSeedHex = (seedHex) => {
  const digest = createHash("sha256")
    .update(Buffer.from(normalizeHex(seedHex), "hex"))
    .digest("hex");

  let scalar = BigInt(`0x${digest}`) % ORDER;
  if (scalar === 0n) scalar = 1n;

  return keypairFromScalar(scalar);
};

export const buildPrivacyRootSigningMessage = ({
  address,
  transparentPubKeyHex,
  transparentPubKeyBytes,
}) => {
  const trimmedAddress = address?.trim();
  if (!trimmedAddress) {
    throw new Error("address is required");
  }

  const pubKeyHex = pickMaterialHex(
    { transparentPubKeyHex, transparentPubKeyBytes },
    "transparentPubKeyHex",
    "transparentPubKeyBytes",
    "transparent pubkey",
  );

  return Buffer.from(
    `${ROOT_SIGNING_DOMAIN}\naddress:${trimmedAddress}\npubkey:${pubKeyHex}`,
    "utf8",
  );
};

export const keypairFromPrivacyRootMaterial = ({
  address,
  transparentPubKeyHex,
  transparentPubKeyBytes,
  signatureHex,
  signatureBytes,
}) => {
  const trimmedAddress = address?.trim();
  if (!trimmedAddress) {
    throw new Error("address is required");
  }

  const pubKeyHex = pickMaterialHex(
    { transparentPubKeyHex, transparentPubKeyBytes },
    "transparentPubKeyHex",
    "transparentPubKeyBytes",
    "transparent pubkey",
  );
  const signature = pickMaterialHex(
    { signatureHex, signatureBytes },
    "signatureHex",
    "signatureBytes",
    "signature",
  );

  const rootSeedHex = createHash("sha256")
    .update(
      Buffer.from(
        `${ROOT_SIGNING_DOMAIN}/root\naddress:${trimmedAddress}\npubkey:${pubKeyHex}\nsignature:${signature}`,
        "utf8",
      ),
    )
    .digest("hex");

  const disclosureSeedHex = createHash("sha256")
    .update(
      Buffer.from(
        `${ROOT_SIGNING_DOMAIN}/derive\ndomain:${DISCLOSURE_DOMAIN}\nroot:${rootSeedHex}`,
        "utf8",
      ),
    )
    .digest("hex");

  let scalar = BigInt(`0x${disclosureSeedHex}`) % ORDER;
  if (scalar === 0n) scalar = 1n;

  return keypairFromScalar(scalar);
};

export const keypairFromPrivacyRootSigner = async (signer) => {
  if (!signer) {
    throw new Error("privacy root signer is required");
  }

  const address =
    typeof signer.address === "function" ? await signer.address() : signer.address;
  const transparentPubKey =
    typeof signer.pubKeyBytes === "function"
      ? await signer.pubKeyBytes()
      : signer.pubKeyBytes ?? signer.transparentPubKeyBytes ?? signer.transparentPubKeyHex;

  const signingMessage = buildPrivacyRootSigningMessage({
    address,
    transparentPubKeyBytes: transparentPubKey,
  });

  if (typeof signer.signPrivacyRoot !== "function") {
    throw new Error("signer.signPrivacyRoot(messageBytes) is required");
  }

  const signature = await signer.signPrivacyRoot(signingMessage);

  return keypairFromPrivacyRootMaterial({
    address,
    transparentPubKeyBytes: transparentPubKey,
    signatureBytes: signature,
  });
};

export const randomKeypair = () => {
  const max = 1n << 256n;
  const limit = max - (max % ORDER);

  while (true) {
    const candidate = BigInt(`0x${randomBytes(32).toString("hex")}`);
    if (candidate >= limit) continue;

    const scalar = candidate % ORDER;
    if (scalar !== 0n) return keypairFromScalar(scalar);
  }
};

const isDirectRun =
  process.argv[1] !== undefined &&
  import.meta.url === pathToFileURL(process.argv[1]).href;

if (isDirectRun) {
  console.log(
    "deterministic private:",
    keypairFromPrivateKeyHex("01".padStart(64, "0")),
  );
  console.log(
    "deterministic seed:",
    keypairFromSeedHex("deadbeef".padStart(64, "0")),
  );
  console.log(
    "deterministic signer material:",
    keypairFromPrivacyRootMaterial({
      address: "clair1auditor",
      transparentPubKeyHex: "010203",
      signatureHex: "040506",
    }),
  );
  console.log("random:", randomKeypair());
}
