import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

// Workspace-wide flat config (re-exported by the root eslint.config.mjs). It lives here
// because this standalone npm project pins the TypeScript copy typescript-eslint can parse
// with — see tools/lint/package.json. Covers packages/common/{ts,ui} and apps/*/frontend-web.
export default tseslint.config(
  {
    ignores: ["**/dist/**", "**/node_modules/**", "**/*.tsbuildinfo", "tools/**", "eslint.config.mjs"],
  },
  js.configs.recommended,
  // Recommended = syntactic rules only; the type-checked presets are deliberately not used.
  tseslint.configs.recommended,
  reactHooks.configs.flat.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: { globals: globals.browser },
  },
  {
    rules: {
      // A leading underscore marks a deliberately unused parameter, catch binding or variable;
      // rest siblings are how props are dropped before spreading the remainder.
      "@typescript-eslint/no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          varsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_",
          ignoreRestSiblings: true,
        },
      ],
      // Tests assert on Intl output that contains NBSP / U+2212, written as regex literals.
      "no-irregular-whitespace": ["error", { skipRegExps: true, skipStrings: true, skipTemplates: true }],
      // Downgraded to warn (cleanup tracked separately): every current hit is the
      // `zodResolver(schema as any) as any` / `handleSubmit(fn as any)` workaround for
      // react-hook-form's input/output generics, plus a few untyped recharts tooltip props.
      "@typescript-eslint/no-explicit-any": "warn",
      // Downgraded to warn: React Compiler's strict "no setState in an effect" check. The flagged
      // effects are working "reset dependent toggle when its parent toggles off" patterns;
      // rewriting them as derived state is a refactor, not a lint fix.
      "react-hooks/set-state-in-effect": "warn",
    },
  },
  {
    // Config and tool entry points run under Node; the service worker runs in a ServiceWorkerGlobalScope.
    files: ["**/*.config.{js,mjs,ts,mts}", "vitest.setup.ts"],
    languageOptions: { globals: globals.node },
  },
  {
    files: ["**/public/sw.js"],
    languageOptions: { globals: globals.serviceworker },
  },
);
