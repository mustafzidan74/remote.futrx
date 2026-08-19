import assert from "node:assert/strict";
import test from "node:test";
import {
  describeTls,
  emptySiteForm,
  formToInput,
  formatAgo,
  formatCountdown,
  formatMs,
  formatUptime,
  normalizeSiteUrl,
  parsePastedUrls,
  siteDot,
  siteName,
  siteToForm,
  sortSites,
  sparkline,
  summarizeSites,
  validateSiteForm,
  type SiteForm,
} from "./clientSitesState.ts";
import type { WatchedSiteView } from "../../models/sitewatch.ts";

function view(overrides: Partial<WatchedSiteView> = {}): WatchedSiteView {
  return {
    id: "aaaaaaaaaaaaaaaaaaaaaaaa",
    label: "",
    url: "https://shop.example.com/",
    enabled: true,
    intervalMinutes: 5,
    checks: { status: {}, tls: { warnDays: 21 } },
    notify: true,
    method: "HEAD",
    status: "up",
    uptime: { checks: 0 },
    ...overrides,
  };
}

function form(overrides: Partial<SiteForm> = {}): SiteForm {
  return { ...emptySiteForm(), url: "shop.example.com", ...overrides };
}

test("siteName falls back to the hostname when there is no label", () => {
  assert.equal(siteName(view()), "shop.example.com");
  assert.equal(siteName(view({ label: "  Client shop  " })), "Client shop");
  assert.equal(siteName({ url: "not a url" }), "not a url");
});

test("siteDot maps status to a tone and carries the failure into the tooltip", () => {
  assert.deepEqual(siteDot(view({ status: "up" })), {
    tone: "green",
    label: "Up",
    title: "Up",
  });
  assert.equal(siteDot(view({ status: "slow" })).tone, "amber");
  assert.equal(siteDot(view({ status: "unknown" })).tone, "grey");

  const down = siteDot(view({ status: "down", lastError: "answered HTTP 502" }));
  assert.equal(down.tone, "red");
  assert.equal(down.title, "Down\nanswered HTTP 502");
});

test("a paused site is grey whatever its last reading was", () => {
  const dot = siteDot(view({ enabled: false, status: "down", lastError: "answered HTTP 502" }));
  assert.equal(dot.tone, "grey");
  assert.equal(dot.label, "Paused");
});

test("sortSites lifts what needs attention to the top, then sorts by name", () => {
  const sites = [
    view({ id: "1", label: "Zulu", status: "up" }),
    view({ id: "2", label: "Alpha", status: "up" }),
    view({ id: "3", label: "Bravo", status: "down" }),
    view({ id: "4", label: "Charlie", status: "slow" }),
    view({ id: "5", label: "Delta", status: "unknown" }),
    view({ id: "6", label: "Echo", status: "down", enabled: false }),
  ];
  assert.deepEqual(
    sortSites(sites).map((site) => site.label),
    ["Bravo", "Charlie", "Alpha", "Zulu", "Delta", "Echo"],
  );
  // The input is not mutated: the table re-sorts a fresh payload every minute.
  assert.equal(sites[0].label, "Zulu");
});

test("formatUptime distinguishes a missing window from zero availability", () => {
  assert.equal(formatUptime(undefined), "—");
  assert.equal(formatUptime(0), "0%");
  assert.equal(formatUptime(100), "100%");
  assert.equal(formatUptime(99.95), "99.95%");
  assert.equal(formatUptime(Number.NaN), "—");
});

test("formatMs picks the unit that keeps a digit", () => {
  assert.equal(formatMs(undefined), "—");
  assert.equal(formatMs(0), "—");
  assert.equal(formatMs(240), "240 ms");
  assert.equal(formatMs(1200), "1.2 s");
  assert.equal(formatMs(15_000), "15 s");
});

test("describeTls colours by the site's own warning window", () => {
  assert.deepEqual(describeTls(view()), { label: "—", tone: "grey" });
  assert.deepEqual(describeTls(view({ tlsDaysLeft: 60 })), { label: "60 d", tone: "green" });
  assert.deepEqual(describeTls(view({ tlsDaysLeft: 9 })), { label: "9 d", tone: "amber" });
  assert.deepEqual(describeTls(view({ tlsDaysLeft: 0 })), { label: "today", tone: "red" });
  assert.deepEqual(describeTls(view({ tlsDaysLeft: -3 })), { label: "expired", tone: "red" });
  // A stricter site warns earlier for the same certificate.
  const strict = view({ tlsDaysLeft: 40, checks: { status: {}, tls: { warnDays: 45 } } });
  assert.equal(describeTls(strict).tone, "amber");
  // warnDays 0 disables the rule, so the same reading stays green.
  const off = view({ tlsDaysLeft: 2, checks: { status: {}, tls: { warnDays: 0 } } });
  assert.equal(describeTls(off).tone, "green");
});

test("countdown and age read as English rather than as numbers", () => {
  const now = 1_000_000_000;
  assert.equal(formatCountdown(undefined, now), "—");
  assert.equal(formatCountdown(now - 1000, now), "due now");
  assert.equal(formatCountdown(now + 4 * 60_000, now), "in 4 min");
  assert.equal(formatAgo(undefined, now), "never");
  assert.equal(formatAgo(now - 5_000, now), "just now");
  assert.equal(formatAgo(now - 5 * 60_000, now), "5 min ago");
});

test("sparkline draws the successes and marks the failures as gaps", () => {
  const chart = sparkline([100, 0, 200, 400], 90, 20);
  assert.equal(chart.peakMs, 400);
  assert.deepEqual(chart.failures, [30]);
  // The line breaks at the failure and starts a new segment afterwards.
  assert.equal(chart.path.split("M").length - 1, 2);
  assert.ok(chart.path.startsWith("M0 "));
  // The slowest sample sits nearest the top of the box.
  const slowest = chart.path.split(" ").at(-1);
  assert.equal(slowest, "1");
});

test("sparkline says nothing when there is nothing to draw", () => {
  assert.equal(sparkline(undefined).path, "");
  assert.equal(sparkline([]).path, "");
  // A history of nothing but failures is all gaps and no line.
  const allFailed = sparkline([0, 0, 0]);
  assert.equal(allFailed.path, "");
  assert.equal(allFailed.failures.length, 3);
});

test("normalizeSiteUrl mirrors the backend's rule", () => {
  assert.equal(normalizeSiteUrl("shop.example.com"), "https://shop.example.com/");
  assert.equal(normalizeSiteUrl("  http://blog.example.com/x  "), "http://blog.example.com/x");
  assert.equal(normalizeSiteUrl(""), undefined);
  assert.equal(normalizeSiteUrl("ftp://files.example.com"), undefined);
  assert.equal(normalizeSiteUrl("localhost:3000"), undefined);
  assert.equal(normalizeSiteUrl("not a url"), undefined);
});

test("parsePastedUrls drops comments, blanks and repeats", () => {
  assert.deepEqual(
    parsePastedUrls(
      "shop.example.com\nhttps://shop.example.com/  # already listed\n# a comment\n\napp.example.com, cdn.example.com\nlocalhost:3000",
    ),
    [
      "https://shop.example.com/",
      "https://app.example.com/",
      "https://cdn.example.com/",
    ],
  );
});

test("validateSiteForm refuses what the API would refuse", () => {
  assert.equal(validateSiteForm(form()), undefined);
  assert.match(validateSiteForm(form({ url: "" })) ?? "", /address/);
  assert.match(validateSiteForm(form({ url: "localhost:3000" })) ?? "", /address/);
  assert.match(validateSiteForm(form({ intervalMinutes: 0 })) ?? "", /between 1 and 60/);
  assert.match(validateSiteForm(form({ intervalMinutes: 61 })) ?? "", /between 1 and 60/);
  assert.match(validateSiteForm(form({ expectStatus: "99" })) ?? "", /HTTP code/);
  assert.equal(validateSiteForm(form({ expectStatus: "200" })), undefined);
  assert.match(validateSiteForm(form({ maxResponseMs: "-5" })) ?? "", /positive number/);
  assert.match(
    validateSiteForm(form({ extraUrls: [{ label: "", url: "nope" }] })) ?? "",
    /not a usable address/,
  );
  assert.match(
    validateSiteForm(form({ headers: [{ name: "Bad Header", value: "x" }] })) ?? "",
    /not a valid header name/,
  );
  assert.match(
    validateSiteForm(
      form({ extraUrls: Array.from({ length: 6 }, () => ({ label: "", url: "a.example.com" })) }),
    ) ?? "",
    /at most 5 extra URLs/,
  );
});

test("the form bounds come from the server when it sent them", () => {
  assert.equal(validateSiteForm(form({ intervalMinutes: 90 }), { min: 1, max: 120 }), undefined);
});

test("formToInput builds the body the API expects", () => {
  const input = formToInput(
    form({
      label: "  Client shop  ",
      url: "  shop.example.com  ",
      intervalMinutes: 10,
      method: "GET",
      expectStatus: "200",
      mustContain: " Add to cart ",
      maxResponseMs: "2000",
      tlsWarnDays: 30,
      projectId: " proj-1 ",
      notify: false,
      extraUrls: [
        { label: " Checkout ", url: " shop.example.com/checkout " },
        { label: "", url: "   " },
      ],
      headers: [
        { name: " X-Token ", value: " secret " },
        { name: "", value: "dropped" },
      ],
    }),
  );

  assert.equal(input.label, "Client shop");
  assert.equal(input.url, "shop.example.com");
  assert.equal(input.method, "GET");
  assert.equal(input.intervalMinutes, 10);
  assert.equal(input.projectId, "proj-1");
  assert.equal(input.notify, false);
  assert.equal(input.checks.status.expect, 200);
  assert.equal(input.checks.maxResponseMs, 2000);
  assert.equal(input.checks.tls.warnDays, 30);
  assert.deepEqual(input.checks.keyword, { mustContain: "Add to cart" });
  assert.deepEqual(input.headers, { "X-Token": "secret" });
  // Blank rows are dropped, and an extra URL inherits the site's own rules.
  assert.equal(input.extraUrls?.length, 1);
  assert.equal(input.extraUrls?.[0].url, "shop.example.com/checkout");
  assert.deepEqual(input.extraUrls?.[0].checks, input.checks);
});

test("formToInput omits an empty keyword rather than sending a blank one", () => {
  const input = formToInput(form());
  assert.equal(input.checks.keyword, undefined);
  assert.equal(input.checks.status.expect, undefined);
  assert.equal(input.checks.maxResponseMs, undefined);
  assert.equal(input.headers, undefined);
});

test("siteToForm round-trips a stored site", () => {
  const stored = view({
    label: "Client shop",
    method: "GET",
    checks: {
      status: { expect: 200 },
      tls: { warnDays: 14 },
      maxResponseMs: 1500,
      keyword: { mustContain: "cart", mustNotContain: "error" },
    },
    extraUrls: [{ label: "Checkout", url: "https://shop.example.com/checkout", checks: { status: {}, tls: { warnDays: 14 } } }],
    headers: { "X-Token": "secret" },
    projectId: "proj-1",
  });
  const restored = siteToForm(stored);
  assert.equal(restored.label, "Client shop");
  assert.equal(restored.method, "GET");
  assert.equal(restored.expectStatus, "200");
  assert.equal(restored.maxResponseMs, "1500");
  assert.equal(restored.tlsWarnDays, 14);
  assert.equal(restored.mustContain, "cart");
  assert.equal(restored.mustNotContain, "error");
  assert.equal(restored.projectId, "proj-1");
  assert.deepEqual(restored.extraUrls, [{ label: "Checkout", url: "https://shop.example.com/checkout" }]);
  assert.deepEqual(restored.headers, [{ name: "X-Token", value: "secret" }]);
});

test("summarizeSites says what is wrong, or that nothing is", () => {
  assert.equal(summarizeSites([]), "No sites are being watched yet.");
  assert.equal(summarizeSites([view()]), "1 site · all up");
  assert.equal(
    summarizeSites([
      view({ id: "1", status: "down" }),
      view({ id: "2", status: "slow" }),
      view({ id: "3", status: "up" }),
      view({ id: "4", status: "up", enabled: false }),
    ]),
    "4 sites · 1 down · 1 slow · 1 paused",
  );
});
