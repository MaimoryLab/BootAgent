import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { EditionTag } from "./EditionTag";

describe("EditionTag", () => {
  it("labels the two regional builds", () => {
    render(<EditionTag edition="cn" />);
    expect(screen.getByText("国内版")).toBeTruthy();
    render(<EditionTag edition="intl" />);
    expect(screen.getByText("国际版")).toBeTruthy();
  });

  // Most desktop Agents ship a single build, so the tag has to disappear rather
  // than leave an empty box beside every name.
  it("renders nothing without a known edition", () => {
    expect(render(<EditionTag edition={undefined} />).container.innerHTML).toBe("");
    expect(render(<EditionTag edition="apac" />).container.innerHTML).toBe("");
  });
});
