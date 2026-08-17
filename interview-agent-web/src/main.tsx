import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app/App";
import { AppStoreProvider } from "./store/app-store";
import "./styles.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AppStoreProvider>
      <App />
    </AppStoreProvider>
  </StrictMode>,
);
