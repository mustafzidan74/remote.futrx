import assert from "node:assert/strict";
import test from "node:test";
import {
  describeLastPing,
  healthCheckUrl,
  isAbsoluteHttpUrl,
  minutesUntilNextPing,
  validateHeartbeatForm,
  type HeartbeatForm,
} from "./monitoringState.ts";
import type { MonitoringSettings } from "../../models/monitoring.ts";

function form(overrides: Partial<HeartbeatForm> = {}): HeartbeatForm {
  return {
    enabled: false,
    heartbeatUrl: "",
    intervalMinutes: 5,
    configured: false,
    ...overrides,
  };
}

function settings(overrides: Partial<MonitoringSettings> = {}): MonitoringSettings {
  return {
    enabled: true,
    configured: true,
    intervalMinutes: 5,
    minIntervalMinutes: 1,
    maxIntervalMinutes: 60,
    healthPath: "/healthz",
    ...overrides,
  };
}

test("isAbsoluteHttpUrl accepts only absolute http(s) URLs", () => {
  const cases: Array<[string, boolean]> = [
    ["https://hc-ping.com/9f3a1c72-5b6d", true],
    ["http://127.0.0.1:8080/ping", true],
    ["  https://hc-ping.com/token  ", true],
    ["/healthz", false],
    ["hc-ping.com/token", false],
    ["ftp://hc-ping.com/token", false],
    ["javascript:alert(1)", false],
    ["", false],
  ];
  for (const [input, want] of cases) {
    assert.equal(isAbsoluteHttpUrl(input), want, `isAbsoluteHttpUrl(${JSON.stringify(input)})`);
  }
});

test("validateHeartbeatForm mirrors the backend's rules", () => {
  assert.equal(validateHeartbeatForm(form()), undefined);
  assert.equal(
    validateHeartbeatForm(form({ enabled: true, heartbeatUrl: "https://hc-ping.com/token" })),
    undefined,
  );

  // A blank URL over a stored one means "keep it", so enabling is allowed.
  assert.equal(validateHeartbeatForm(form({ enabled: true, configured: true })), undefined);

  assert.match(
    validateHeartbeatForm(form({ heartbeatUrl: "hc-ping.com/token" })) ?? "",
    /absolute http\(s\) URL/,
  );
  assert.match(
    validateHeartbeatForm(form({ enabled: true })) ?? "",
    /Paste a heartbeat URL/,
  );
});

test("validateHeartbeatForm bounds the interval by what the server sent", () => {
  const valid = form({ heartbeatUrl: "https://hc-ping.com/token" });

  assert.equal(validateHeartbeatForm({ ...valid, intervalMinutes: 1 }), undefined);
  assert.equal(validateHeartbeatForm({ ...valid, intervalMinutes: 60 }), undefined);

  assert.match(validateHeartbeatForm({ ...valid, intervalMinutes: 0 }) ?? "", /between 1 and 60/);
  assert.match(validateHeartbeatForm({ ...valid, intervalMinutes: 61 }) ?? "", /between 1 and 60/);
  assert.match(validateHeartbeatForm({ ...valid, intervalMinutes: 2.5 }) ?? "", /between 1 and 60/);
  assert.match(
    validateHeartbeatForm({ ...valid, intervalMinutes: 20 }, { min: 5, max: 15 }) ?? "",
    /between 5 and 15/,
  );
});

test("describeLastPing separates never-tried from delivered and failed", () => {
  assert.deepEqual(describeLastPing(null), { tone: "idle", label: "No heartbeat sent yet" });
  assert.deepEqual(describeLastPing(settings()), { tone: "idle", label: "No heartbeat sent yet" });

  assert.deepEqual(describeLastPing(settings({ lastPingAt: 1755, lastPingStatus: "ok" })), {
    tone: "ok",
    label: "Delivered",
    at: 1755,
  });

  assert.deepEqual(
    describeLastPing(
      settings({
        lastPingAt: 1755,
        lastPingStatus: "failed",
        lastPingError: "the heartbeat URL responded 404",
      }),
    ),
    {
      tone: "failed",
      label: "Failed",
      at: 1755,
      detail: "the heartbeat URL responded 404",
    },
  );
});

test("minutesUntilNextPing counts down from the last attempt, not the last success", () => {
  const now = 1_700_000_000_000;

  // Nothing scheduled at all.
  assert.equal(minutesUntilNextPing(null, now), undefined);
  assert.equal(minutesUntilNextPing(settings({ enabled: false }), now), undefined);
  assert.equal(minutesUntilNextPing(settings({ configured: false }), now), undefined);

  // Armed but never pushed: the next tick sends.
  assert.equal(minutesUntilNextPing(settings(), now), 0);

  assert.equal(minutesUntilNextPing(settings({ lastPingAt: now }), now), 5);
  assert.equal(minutesUntilNextPing(settings({ lastPingAt: now - 120_000 }), now), 3);

  // A failed attempt throttles exactly like a successful one.
  assert.equal(
    minutesUntilNextPing(settings({ lastPingAt: now - 60_000, lastPingStatus: "failed" }), now),
    4,
  );

  // Overdue never goes negative.
  assert.equal(minutesUntilNextPing(settings({ lastPingAt: now - 3_600_000 }), now), 0);
});

test("healthCheckUrl joins the origin and the server-reported path exactly once", () => {
  assert.equal(healthCheckUrl("https://remote.example.com", "/healthz"), "https://remote.example.com/healthz");
  assert.equal(healthCheckUrl("https://remote.example.com/", "/healthz"), "https://remote.example.com/healthz");
  assert.equal(healthCheckUrl("https://remote.example.com//", "healthz"), "https://remote.example.com/healthz");
});
