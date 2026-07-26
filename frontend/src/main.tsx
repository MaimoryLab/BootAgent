import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { HashRouter } from "react-router-dom";

import App from "./App";
import { WizardProvider } from "./state/WizardContext";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/app.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <HashRouter>
      <WizardProvider>
        <App />
      </WizardProvider>
    </HashRouter>
  </StrictMode>,
);
