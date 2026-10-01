// @hutch cli=0.27.1 cottontail=0.7.1
export default {
  // The repo is a Bun workspace; let Bun do the installs so `workspace:*`
  // dependencies on @screentime/shared resolve.
  packageManager: 'bun',
  // Build steps live in package.json: Hutch's script shell can't see
  // workspace-hoisted binaries such as vite.
  electrobun: {
    version: '2.0.2',
  },
};
