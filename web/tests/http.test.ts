import { strict as assert } from "node:assert";
import test from "node:test";

import { parseResponseBody } from "../src/lib/http.ts";

test("204 and 205 responses decode to null instead of throwing", () => {
  // Delete endpoints answer 204 with an empty body; parsing it as JSON used to
  // surface a failure for an operation the server had already applied.
  assert.equal(parseResponseBody(204, ""), null);
  assert.equal(parseResponseBody(205, ""), null);
  assert.equal(parseResponseBody(200, "   "), null);
});

test("a JSON body is decoded unchanged", () => {
  assert.deepEqual(parseResponseBody(200, '{"saved":true}'), { saved: true });
  assert.deepEqual(parseResponseBody(201, "[1,2]"), [1, 2]);
});

test("a malformed body still throws so the caller can report it", () => {
  assert.throws(() => parseResponseBody(200, "<html>oops</html>"));
});
