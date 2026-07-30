import type { PropsWithChildren } from "react";

import { NavigationSidebar } from "./NavigationSidebar";

export function AppWindow({ children }: PropsWithChildren) {
  return (
    <div className="desktop-stage">
      <div className="app-window">
        <NavigationSidebar />
        <main className="app-main">{children}</main>
      </div>
    </div>
  );
}
