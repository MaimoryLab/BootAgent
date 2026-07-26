import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// vitest runs without injected globals, so RTL's built-in auto-cleanup (which
// registers on a global afterEach) never engages; do it explicitly.
afterEach(() => cleanup());
