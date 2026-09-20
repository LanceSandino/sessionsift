/** @module WebEntrypoint @description Boots the SessionSift React application. */
import React from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";

const root = document.getElementById("root");
if (root) createRoot(root).render(<App />);
