import assert from "node:assert/strict";
import test from "node:test";
import type { ProjectPortal } from "../../models/project.ts";
import {
  DEFAULT_PORTAL_FORM,
  describePortal,
  portalFormFrom,
  portalUpdateInput,
} from "./projectPortalState.ts";

function portal(overrides: Partial<ProjectPortal> = {}): ProjectPortal {
  return {
    enabled: true,
    showPreview: true,
    showChangelog: true,
    showUsage: false,
    ...overrides,
  };
}

test("a project with no stored portal starts closed but pre-configured", () => {
  const form = portalFormFrom(null);
  assert.deepEqual(form, DEFAULT_PORTAL_FORM);
  assert.equal(form.enabled, false);
  assert.equal(form.showPreview, true);
  assert.equal(form.showChangelog, true);
  assert.equal(form.showUsage, false);
});

test("adopting a server response keeps every toggle and text field", () => {
  const form = portalFormFrom(
    portal({
      showChangelog: false,
      showUsage: true,
      brandTitle: "Acme Shop",
      note: "line one\nline two",
      url: "https://remote.example.com/portal/abcd1234?t=secret",
    }),
  );

  assert.deepEqual(form, {
    enabled: true,
    showPreview: true,
    showChangelog: false,
    showUsage: true,
    brandTitle: "Acme Shop",
    note: "line one\nline two",
  });
});

test("the write payload trims the brand title but preserves note line breaks", () => {
  const input = portalUpdateInput({
    ...DEFAULT_PORTAL_FORM,
    enabled: true,
    brandTitle: "  Acme Shop  ",
    note: "line one\nline two",
  });

  assert.equal(input.brandTitle, "Acme Shop");
  assert.equal(input.note, "line one\nline two");
});

test("overrides drive the enable, rotate, and disable actions", () => {
  const form = { ...DEFAULT_PORTAL_FORM, enabled: false };

  assert.equal(portalUpdateInput(form, { enabled: true }).enabled, true);
  assert.equal(portalUpdateInput(form, { enabled: true, rotate: true }).rotate, true);
  assert.equal(portalUpdateInput({ ...form, enabled: true }, { enabled: false }).enabled, false);
});

test("rotating a disabled portal never asks for a token", () => {
  const input = portalUpdateInput(
    { ...DEFAULT_PORTAL_FORM, enabled: true },
    { enabled: false, rotate: true },
  );
  assert.equal(input.enabled, false);
  assert.equal(input.rotate, false);
});

test("the summary line names exactly what the client will see", () => {
  assert.equal(describePortal(null, true), "Loading the client portal…");
  assert.equal(
    describePortal(null, false),
    "Off — no one outside this project can see anything.",
  );
  assert.equal(
    describePortal(portal({ enabled: false }), false),
    "Off — no one outside this project can see anything.",
  );
  assert.equal(
    describePortal(portal(), false),
    "On — showing preview links, recent changes.",
  );
  assert.equal(
    describePortal(portal({ showUsage: true }), false),
    "On — showing preview links, recent changes, activity.",
  );
  assert.equal(
    describePortal(portal({ showPreview: false, showChangelog: false }), false),
    "On — showing the project name and status only.",
  );
});
