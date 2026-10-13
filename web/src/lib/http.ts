/**
 * Response body decoding for the API client.
 *
 * Several endpoints answer `204 No Content` (deleting content, a tag, a rule set
 * or a schedule item). Calling `response.json()` on an empty body throws, which
 * used to surface as a failure in the UI even though the server had already
 * applied the change. Empty and no-content responses therefore decode to `null`.
 */
export function parseResponseBody(status: number, body: string): unknown {
  if (status === 204 || status === 205) {
    return null;
  }
  const trimmed = body.trim();
  if (!trimmed) {
    return null;
  }
  return JSON.parse(trimmed);
}
