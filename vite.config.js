// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The app is served under /beta/ by cmd/webserver (see internal/webui). Its build
// output goes to internal/webui/dist/, from where the Go binary embeds it. In the
// Toolforge deploy the Node buildpack runs "npm run build" before the Go build.
export default defineConfig({
  root: 'frontend',
  base: '/beta/',
  plugins: [react()],
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: false, // keep internal/webui/dist/.gitignore
  },
});
