/** @module WebEntrypoint @description Applies the viewer's theme, then boots the SessionSift React application. */
import React from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { applyTheme, initialTheme, readStoredTheme, systemPrefersDark } from "./theme";

applyTheme(initialTheme(readStoredTheme(), systemPrefersDark()));
const root = document.getElementById("root");
if (root) createRoot(root).render(<App />);
