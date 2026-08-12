import type { ProfileSummary, ProviderEntry } from "../types/api";

type TransferProfile = Omit<ProfileSummary, "baseUrl">;
type EncryptedKey = { salt: string; iv: string; data: string };
type TransferProvider = Omit<ProviderEntry, "api_key"> & { apikey?: string; key_encrypted?: number };
export type TransferFile = {
  version: 1;
  timestamp: string;
  providers: TransferProvider[];
  profiles: TransferProfile[];
  encrypted: EncryptedKey[];
  mcp?: unknown;
};

/**
 * Typed failures, so TransferPage can localise them.
 *
 * These used to be `new Error("文件格式无效")` -- a Chinese literal outside the
 * i18n table, which an English-locale user saw untranslated, and which
 * describeError passed through verbatim because it is an Error. The page maps
 * these classes to `t()` keys instead of matching on message text.
 */
export class TransferFormatError extends Error {
  constructor() {
    super("transfer file format is invalid");
    this.name = "TransferFormatError";
  }
}

export class TransferVersionError extends Error {
  constructor(readonly found: unknown) {
    super(`unsupported transfer file version: ${String(found)}`);
    this.name = "TransferVersionError";
  }
}

/** A key on a Provider entry means the file was hand-edited or foreign. */
export class TransferProviderShapeError extends Error {
  constructor() {
    super("transfer provider entry carries an inline api_key");
    this.name = "TransferProviderShapeError";
  }
}

/** Raised when AES-GCM refuses the payload, which is almost always a bad password. */
export class TransferPasswordError extends Error {
  constructor(cause: unknown) {
    super("transfer payload could not be decrypted", { cause });
    this.name = "TransferPasswordError";
  }
}

const encode = (value: Uint8Array) => btoa(String.fromCharCode(...value));
const decode = (value: string) => Uint8Array.from(atob(value), (char) => char.charCodeAt(0));

async function keyFrom(password: string, salt: Uint8Array) {
  const material = await crypto.subtle.importKey("raw", new TextEncoder().encode(password), "PBKDF2", false, ["deriveKey"]);
  return crypto.subtle.deriveKey({ name: "PBKDF2", salt: salt as BufferSource, iterations: 100000, hash: "SHA-256" }, material, { name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
}

async function encryptKey(value: string, password: string): Promise<EncryptedKey> {
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const data = await crypto.subtle.encrypt({ name: "AES-GCM", iv: iv as BufferSource }, await keyFrom(password, salt), new TextEncoder().encode(value));
  return { salt: encode(salt), iv: encode(iv), data: encode(new Uint8Array(data)) };
}

async function decryptKey(value: EncryptedKey, password: string): Promise<string> {
  const plain = await crypto.subtle.decrypt({ name: "AES-GCM", iv: decode(value.iv) as BufferSource }, await keyFrom(password, decode(value.salt)), decode(value.data) as BufferSource);
  return new TextDecoder().decode(plain);
}

/** How an export treats the API keys of the Providers it carries. */
export type KeyHandling = "omit" | "plain" | "encrypted";

export async function makeTransfer(
  profiles: ProfileSummary[],
  providers: ProviderEntry[],
  // Defaults to omitting keys. A transfer file describes which Providers and
  // Profiles exist, and that is useful on its own -- carrying live credentials
  // was never required for it and is the part that turns a config file into a
  // secret. The recipient enters their own key, or keeps the one they already
  // have.
  keys: KeyHandling = "omit",
  password = "",
  mcp?: unknown,
): Promise<TransferFile> {
  const encrypted: EncryptedKey[] = [];
  const transferProviders: TransferProvider[] = [];
  for (const provider of providers) {
    const { api_key, ...publicProvider } = provider;
    const item: TransferProvider = { ...publicProvider };
    if (keys === "encrypted" && api_key) item.key_encrypted = encrypted.push(await encryptKey(api_key, password)) - 1;
    // `omit` writes no apikey property at all rather than an empty string, so a
    // reader can tell "this file has no keys" from "this key was blank".
    else if (keys === "plain") item.apikey = api_key || "";
    transferProviders.push(item);
  }
  return {
    version: 1,
    timestamp: new Date().toISOString(),
    providers: transferProviders,
    profiles: profiles.map(({ baseUrl: _baseUrl, ...profile }) => profile),
    encrypted,
    ...(mcp === undefined ? {} : { mcp }),
  };
}

/**
 * Whether importing this file needs a password.
 *
 * The presence of `encrypted` cannot answer this: makeTransfer emits the array
 * either way, empty when encryption was declined, and `[]` is truthy. Reading it
 * as a boolean asked for a password to decrypt nothing, and a user who correctly
 * cancelled that prompt got a silent no-op.
 *
 * Malformed input is not encrypted as far as this is concerned -- returning false
 * lets parseTransfer produce its own error rather than prompting first and
 * failing afterwards.
 */
export function transferNeedsPassword(text: string): boolean {
  try {
    const file = JSON.parse(text) as { encrypted?: unknown; mcp?: { secret_mode?: string } };
    return (Array.isArray(file.encrypted) && file.encrypted.length > 0) || file.mcp?.secret_mode === "encrypted";
  } catch {
    return false;
  }
}

/**
 * Structure check plus the names an import would touch.
 *
 * Separate from parseTransfer because it needs no password: the caller has to be
 * able to reject a malformed or wrong-version file *before* prompting for
 * anything, or the user confirms an overwrite for a file that was never going to
 * load. Throws the same typed errors parseTransfer does.
 */
export function transferSummary(text: string): { providers: string[]; profiles: string[]; carriesKeys: boolean } {
  const file = JSON.parse(text) as Partial<TransferFile>;
  if (file.version !== 1) throw new TransferVersionError(file.version);
  if (!Array.isArray(file.providers) || !Array.isArray(file.profiles) || !Array.isArray(file.encrypted)) {
    throw new TransferFormatError();
  }
  return {
    providers: file.providers.map((provider) => provider.name || provider.id),
    profiles: file.profiles.map((profile) => profile.label || profile.id),
    // Whether this file would replace saved keys. A file exported without keys
    // must leave the recipient's own credentials alone, and the confirmation has
    // to be able to say which kind it is looking at.
    carriesKeys: file.providers.some((provider) =>
      typeof provider.key_encrypted === "number" || typeof provider.apikey === "string",
    ),
  };
}

/** A Provider from a transfer file, plus whether the file supplied its key. */
export type IncomingProvider = ProviderEntry & { carriesKey: boolean };

export async function parseTransfer(text: string, password = ""): Promise<{ providers: IncomingProvider[]; profiles: TransferProfile[]; timestamp: string; mcp?: unknown }> {
  const file = JSON.parse(text) as Partial<TransferFile> & { encrypted?: EncryptedKey[] | { salt: string; iv: string; data: string }; mcp?: unknown };
  // Reported separately: a version mismatch is what a file from another OneAgent
  // build hits, and naming the version found is the difference between "this file
  // is broken" and "this file is newer than this app".
  if (file.version !== 1) throw new TransferVersionError(file.version);
  if (!Array.isArray(file.providers) || !Array.isArray(file.profiles) || !Array.isArray(file.encrypted)) {
    throw new TransferFormatError();
  }
  const encrypted = file.encrypted;
  const profiles = file.profiles.map((profile) => {
    const {
      base_url: _baseUrl,
      baseUrl: _camelBaseUrl,
      api_key: _apiKey,
      apiKey: _camelApiKey,
      hasKey: _hasKey,
      ...rest
    } = profile as TransferProfile & Record<string, unknown>;
    return rest as TransferProfile;
  });
  const providers: IncomingProvider[] = [];
  for (const provider of file.providers) {
    if (Object.hasOwn(provider, "api_key")) throw new TransferProviderShapeError();
    const { apikey, key_encrypted, ...publicProvider } = provider;
    // An absent apikey property is not the same as an empty one: the first means
    // the file was exported without keys and the recipient's own must be kept, the
    // second means the Provider genuinely had none. carriesKey travels with the
    // entry so the caller can pick the right write.
    const carriesKey = typeof key_encrypted === "number" || typeof apikey === "string";
    let apiKey = apikey || "";
    if (typeof key_encrypted === "number") {
      const payload = encrypted[key_encrypted];
      if (!payload) throw new TransferFormatError();
      try {
        apiKey = await decryptKey(payload, password);
      } catch (error) {
        // AES-GCM reports a tag mismatch as a bare OperationError with an empty
        // message, which reached the user as blank text. A wrong password and a
        // corrupt payload are indistinguishable here, so the page says both.
        throw new TransferPasswordError(error);
      }
    }
    providers.push({ ...publicProvider, api_key: apiKey, carriesKey });
  }
  return { providers, profiles, timestamp: file.timestamp || "", mcp: file.mcp };
}

export const stringifyTransfer = (file: TransferFile) => JSON.stringify(file, null, 2);
