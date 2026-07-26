import type { PropsWithChildren } from "react";

import { NavigationSidebar } from "./NavigationSidebar";

export function AppWindow({ children }: PropsWithChildren) {
  return (
    <div className="desktop-stage">
      <div className="app-window">
        <div className="window-chrome window-chrome-sidebar" aria-hidden="true">
          <span className="traffic-light traffic-light-red" />
          <span className="traffic-light traffic-light-yellow" />
          <span className="traffic-light traffic-light-green" />
        </div>
        <div className="window-chrome window-title">OneAgent</div>
        <NavigationSidebar />
        <main className="app-main">{children}</main>
      </div>
    </div>
  );
}
