import { beforeEach, describe, expect, it } from "vitest";

import { migrateStorageKeys } from "./storageMigration";

describe("migrateStorageKeys", () => {
  beforeEach(() => localStorage.clear());

  it("migrates every launch directory key without skipping adjacent entries", () => {
    for (const id of ["a", "b", "c", "d", "e"]) localStorage.setItem(`oneagent:launch-directory:${id}`, `/tmp/${id}`);
    migrateStorageKeys();
    for (const id of ["a", "b", "c", "d", "e"]) {
      expect(localStorage.getItem(`bootagent:launch-directory:${id}`)).toBe(`/tmp/${id}`);
      expect(localStorage.getItem(`oneagent:launch-directory:${id}`)).toBeNull();
    }
  });
});
