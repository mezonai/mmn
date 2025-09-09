// Keep this package from blocking Nx graph build/lint when local devDeps aren't installed.
// We ignore all files here and rely on the root ESLint setup for linting.
module.exports = {
  root: false,
  ignorePatterns: ['**/*']
};
