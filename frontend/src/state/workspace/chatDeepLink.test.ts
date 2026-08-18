import assert from "node:assert/strict";
import test from "node:test";
import { chatDeepLinkState } from "./chatDeepLink.ts";

test("reads a well-formed chat id from the query string", () => {
  assert.equal(chatDeepLinkState.parse("?chat=abc123"), "abc123");
  assert.equal(chatDeepLinkState.parse("?foo=1&chat=abc123&bar=2"), "abc123");
  assert.equal(chatDeepLinkState.parse("?chat=ABC123"), "abc123");
  assert.equal(chatDeepLinkState.parse("?chat=%20abc123%20"), "abc123");
});

test("ignores anything that is not a chat id", () => {
  assert.equal(chatDeepLinkState.parse(""), null);
  assert.equal(chatDeepLinkState.parse("?other=abc123"), null);
  assert.equal(chatDeepLinkState.parse("?chat="), null);
  assert.equal(chatDeepLinkState.parse("?chat=abc"), null, "too short");
  assert.equal(chatDeepLinkState.parse(`?chat=${"a".repeat(33)}`), null, "too long");
  assert.equal(chatDeepLinkState.parse("?chat=../../etc/passwd"), null);
  assert.equal(chatDeepLinkState.parse("?chat=abc-123"), null, "non-hex characters");
  assert.equal(chatDeepLinkState.parse("?chat=https://evil.example.com"), null);
});

test("strips only the chat parameter when the link has been consumed", () => {
  assert.equal(chatDeepLinkState.withoutChatParam("/", "?chat=abc123", ""), "/");
  assert.equal(
    chatDeepLinkState.withoutChatParam("/", "?chat=abc123&keep=1", ""),
    "/?keep=1"
  );
  assert.equal(chatDeepLinkState.withoutChatParam("/", "", "#top"), "/#top");
  assert.equal(
    chatDeepLinkState.withoutChatParam("/", "?chat=abc123", "#top"),
    "/#top"
  );
});

test("reads the message instant a search hit links to", () => {
  assert.equal(chatDeepLinkState.parseAt("?chat=abc123&at=1712345678901"), 1712345678901);
  assert.equal(chatDeepLinkState.parseAt("?at=%201712345678901%20"), 1712345678901);
});

test("ignores anything that is not a unix-ms timestamp", () => {
  assert.equal(chatDeepLinkState.parseAt(""), null);
  assert.equal(chatDeepLinkState.parseAt("?chat=abc123"), null);
  assert.equal(chatDeepLinkState.parseAt("?at="), null);
  assert.equal(chatDeepLinkState.parseAt("?at=0"), null);
  assert.equal(chatDeepLinkState.parseAt("?at=-5"), null);
  assert.equal(chatDeepLinkState.parseAt("?at=12.5"), null);
  assert.equal(chatDeepLinkState.parseAt("?at=abc"), null);
  assert.equal(chatDeepLinkState.parseAt(`?at=${"9".repeat(16)}`), null, "implausibly wide");
});

test("consuming the link strips the message instant as well", () => {
  assert.equal(
    chatDeepLinkState.withoutChatParam("/", "?chat=abc123&at=1712345678901", ""),
    "/"
  );
  assert.equal(
    chatDeepLinkState.withoutChatParam("/", "?chat=abc123&at=1712345678901&keep=1", ""),
    "/?keep=1"
  );
});
