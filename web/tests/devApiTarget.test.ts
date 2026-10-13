import { strict as assert } from "node:assert";
import test from "node:test";

import { resolveDevApiTarget } from "../vite.config.ts";

test("an unset target falls back to the local Go API", () => {
  assert.equal(resolveDevApiTarget(undefined), "http://localhost:8080");
  assert.equal(resolveDevApiTarget(null), "http://localhost:8080");
  assert.equal(resolveDevApiTarget("   "), "http://localhost:8080");
});

test("a complete URL is used unchanged except for a trailing slash", () => {
  assert.equal(resolveDevApiTarget("http://59.110.90.28:8080"), "http://59.110.90.28:8080");
  assert.equal(resolveDevApiTarget("https://arts.example.com/"), "https://arts.example.com");
});

test("a bare host:port becomes a real URL", () => {
  // Vite cannot proxy to a value without a scheme; the failure would otherwise
  // only appear on the first /api request, far away from the .env edit.
  assert.equal(resolveDevApiTarget("59.110.90.28:8080"), "http://59.110.90.28:8080");
  assert.equal(resolveDevApiTarget("localhost:8080"), "http://localhost:8080");
  assert.equal(resolveDevApiTarget("localhost"), "http://localhost");
});
