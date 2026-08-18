import assert from "node:assert/strict";
import test from "node:test";
import {
  AUTO_TEST_PROMPT,
  SMOKE_TEST_PROMPT,
  canSendUrlCheck,
  urlCheckPrompt,
} from "./testPrompts.ts";

// This exact sentence is what makes an auto-test pass meaningful: an agent
// allowed to weaken its own assertions can always reach PASS.
test("every test prompt forbids loosening assertions", () => {
  const prompts = [
    AUTO_TEST_PROMPT,
    SMOKE_TEST_PROMPT,
    urlCheckPrompt({ url: "http://localhost:3000/cart", expectation: "the total is 42" }),
  ];

  for (const prompt of prompts) {
    assert.ok(prompt.includes("Do not loosen assertions to pass."), prompt);
    assert.ok(prompt.includes("playwright-e2e"), prompt);
  }
});

// The composer's "Test the last change" and the automatic policy must ask for
// the same work, so this string has to stay identical to postrun.AutoTestPrompt.
test("the manual auto-test prompt is the one the driver sends", () => {
  assert.equal(
    AUTO_TEST_PROMPT,
    "Verify the change you just made with Playwright (playwright-e2e skill): " +
      "write or update a minimal e2e spec for the affected user journey against the app on its local port, " +
      "run it headless, and report PASS/FAIL with the assertion output. " +
      "If it fails because of your change, fix the change and re-run once. " +
      "Do not loosen assertions to pass.",
  );
});

test("urlCheckPrompt quotes the target and the expectation", () => {
  const prompt = urlCheckPrompt({
    url: "  https://demo.example.com/checkout  ",
    expectation: "  the order confirmation shows the right total  ",
  });

  assert.ok(prompt.includes("Check https://demo.example.com/checkout with Playwright"));
  assert.ok(prompt.includes("the order confirmation shows the right total"));
});

test("urlCheckPrompt falls back to a sane check when none is given", () => {
  const prompt = urlCheckPrompt({ url: "http://localhost:5173", expectation: "" });

  assert.ok(prompt.includes("the page loads without console errors"));
});

test("canSendUrlCheck needs somewhere to point", () => {
  assert.equal(canSendUrlCheck({ url: "", expectation: "anything" }), false);
  assert.equal(canSendUrlCheck({ url: "   ", expectation: "anything" }), false);
  assert.equal(canSendUrlCheck({ url: "http://localhost:3000", expectation: "" }), true);
});
