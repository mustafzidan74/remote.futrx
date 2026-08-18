/**
 * The prompts behind the composer's Test menu.
 *
 * AUTO_TEST_PROMPT is a verbatim copy of postrun.AutoTestPrompt in
 * backend/internal/service/postrun/prompts.go — "Test the last change" must
 * produce exactly the work the automatic policy produces, or a human check and
 * an automatic one would disagree about what "tested" means. Change both
 * together.
 *
 * All three prompts share two properties worth keeping: they name the
 * `playwright-e2e` skill so the agent picks up the operator's own Playwright
 * conventions, and they forbid weakening assertions to reach a green result.
 */

export const AUTO_TEST_PROMPT =
  "Verify the change you just made with Playwright (playwright-e2e skill): " +
  "write or update a minimal e2e spec for the affected user journey against the app on its local port, " +
  "run it headless, and report PASS/FAIL with the assertion output. " +
  "If it fails because of your change, fix the change and re-run once. " +
  "Do not loosen assertions to pass.";

export const SMOKE_TEST_PROMPT =
  "Run a Playwright smoke suite over the whole app (playwright-e2e skill): " +
  "discover the app on its local port, cover the main user journeys — load, navigation, sign-in if there is one, " +
  "and the primary create/read path — in a small headless spec, and report PASS/FAIL per journey with the assertion output. " +
  "List anything you could not reach and why. Do not loosen assertions to pass.";

export interface UrlCheckInput {
  url: string;
  expectation: string;
}

/**
 * Builds the "Test a URL/flow…" prompt. The URL and the expectation are the
 * user's words, quoted into the prompt rather than interpreted here — deciding
 * what "check the cart total" means is the agent's job, not the composer's.
 */
export function urlCheckPrompt({ url, expectation }: UrlCheckInput): string {
  const target = url.trim();
  const check = expectation.trim();
  const what = check || "the page loads without console errors and its main content renders";
  return (
    `Check ${target} with Playwright (playwright-e2e skill). ` +
    `What to verify: ${what}. ` +
    "Write a minimal headless spec for it, run it, and report PASS/FAIL with the assertion output " +
    "plus any console or network errors you saw. Do not loosen assertions to pass."
  );
}

/** A URL check needs somewhere to point; the expectation may be left to the agent. */
export function canSendUrlCheck(input: UrlCheckInput): boolean {
  return input.url.trim().length > 0;
}

/**
 * The prompt behind `/test [what]`.
 *
 * Three shapes, because those are the three things a person means when they
 * type it: nothing at all is "check what you just did", something that looks
 * like an address is a page to point at, and anything else is a journey
 * described in words. Deciding which is which is the only judgement made here;
 * what the words mean is left to the agent.
 */
export function testCommandPrompt(arg: string): string {
  const value = arg.trim();
  if (!value) return AUTO_TEST_PROMPT;
  if (looksLikeUrl(value)) return urlCheckPrompt({ url: value, expectation: "" });
  return flowCheckPrompt(value);
}

/** A described journey rather than a single address. */
export function flowCheckPrompt(description: string): string {
  return (
    `Check this with Playwright (playwright-e2e skill): ${description.trim()}. ` +
    "Find the app on its local port, write a minimal headless spec for that journey, run it, " +
    "and report PASS/FAIL with the assertion output plus any console or network errors you saw. " +
    "Do not loosen assertions to pass."
  );
}

/**
 * Whether a `/test` argument is an address rather than a description. A bare
 * `localhost:3000` and a bare `/checkout` both count: the agent still has to
 * resolve them against the running app, but they are places, not journeys.
 */
export function looksLikeUrl(value: string): boolean {
  const trimmed = value.trim();
  if (/\s/.test(trimmed)) return false;
  return (
    /^https?:\/\//i.test(trimmed) ||
    /^localhost(:\d+)?(\/|$)/i.test(trimmed) ||
    /^127\.0\.0\.1(:\d+)?(\/|$)/.test(trimmed) ||
    /^\//.test(trimmed) ||
    /^[a-z0-9-]+(\.[a-z0-9-]+)+(:\d+)?(\/|$)/i.test(trimmed)
  );
}
