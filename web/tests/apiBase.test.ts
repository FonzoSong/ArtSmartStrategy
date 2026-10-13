import { strict as assert } from "node:assert";
import test from "node:test";

import { resolveAPIBaseURL } from "../src/lib/apiBase.ts";

test("an empty base URL keeps the browser on the same origin", () => {
  // Same-origin is the default for both the dev server proxy and Nginx.
  assert.equal(resolveAPIBaseURL(undefined), "");
  assert.equal(resolveAPIBaseURL(""), "");
  assert.equal(resolveAPIBaseURL("   "), "");
});

test("an absolute base URL is used unchanged except for a trailing slash", () => {
  assert.equal(resolveAPIBaseURL("http://localhost:8080/"), "http://localhost:8080");
  assert.equal(resolveAPIBaseURL("https://arts.example.com"), "https://arts.example.com");
});

test("a bare host or host:port becomes a real URL", () => {
  // A Docker-internal service name can never be resolved by a browser; turning
  // it into an absolute URL keeps the failure visible instead of silently
  // requesting a path under the web root.
  assert.equal(resolveAPIBaseURL("localhost:8080"), "http://localhost:8080");
  assert.equal(resolveAPIBaseURL("192.168.1.10:8080"), "http://192.168.1.10:8080");
  assert.equal(resolveAPIBaseURL("api:8080"), "http://api:8080");
});

test("a path prefix stays a path prefix", () => {
  assert.equal(resolveAPIBaseURL("/gateway"), "/gateway");
  assert.equal(resolveAPIBaseURL("gateway/"), "/gateway");
});
