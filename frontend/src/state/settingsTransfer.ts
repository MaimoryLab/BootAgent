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
};

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

export async function makeTransfer(profiles: ProfileSummary[], providers: ProviderEntry[], encrypt: boolean, password = ""): Promise<TransferFile> {
  const encrypted: EncryptedKey[] = [];
  const transferProviders: TransferProvider[] = [];
  for (const provider of providers) {
    const { api_key, ...publicProvider } = provider;
    const item: TransferProvider = { ...publicProvider };
    if (encrypt && api_key) item.key_encrypted = encrypted.push(await encryptKey(api_key, password)) - 1;
    else item.apikey = api_key || "";
    transferProviders.push(item);
  }
  return {
    version: 1,
    timestamp: new Date().toISOString(),
    providers: transferProviders,
    profiles: profiles.map(({ baseUrl: _baseUrl, ...profile }) => profile),
    encrypted,
  };
}

export async function parseTransfer(text: string, password = ""): Promise<{ providers: ProviderEntry[]; profiles: TransferProfile[]; timestamp: string }> {
  const file = JSON.parse(text) as Partial<TransferFile> & { encrypted?: EncryptedKey[] | { salt: string; iv: string; data: string } };
  if (file.version !== 1 || !Array.isArray(file.providers) || !Array.isArray(file.profiles)) throw new Error("文件格式无效");
  if (!Array.isArray(file.encrypted)) throw new Error("文件格式无效");
  const encrypted = file.encrypted;
  const profiles = file.profiles.map((profile) => {
    if (Object.hasOwn(profile, "base_url") || Object.hasOwn(profile, "baseUrl")) throw new Error("Profile 文件格式无效");
    return profile;
  });
  const providers: ProviderEntry[] = [];
  for (const provider of file.providers) {
    if (Object.hasOwn(provider, "api_key")) throw new Error("Provider 文件格式无效");
    const { apikey, key_encrypted, ...publicProvider } = provider;
    const apiKey = typeof key_encrypted === "number" ? await decryptKey(encrypted[key_encrypted], password) : apikey || "";
    providers.push({ ...publicProvider, api_key: apiKey });
  }
  return { providers, profiles, timestamp: file.timestamp || "" };
}

export const stringifyTransfer = (file: TransferFile) => JSON.stringify(file, null, 2);
