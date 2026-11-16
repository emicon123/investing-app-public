// ESLint flat config for the frontend workspace. The rules live in tools/lint (its own
// npm project, because typescript-eslint needs a TypeScript 6 copy next to the apps' TypeScript 7).
export { default } from "./tools/lint/eslint.config.js";
