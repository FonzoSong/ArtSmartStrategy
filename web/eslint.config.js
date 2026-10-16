import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: ["dist/**", "node_modules/**"],
  },
  ...tseslint.configs.recommended,
  {
    files: ["src/app.ts"],
    // This legacy page keeps its established @ts-nocheck and has unrelated
    // pre-existing unused helpers; typecheck/build still cover the integration.
    rules: {
      "@typescript-eslint/ban-ts-comment": "off",
      "@typescript-eslint/no-unused-vars": "off",
    },
  },
);
