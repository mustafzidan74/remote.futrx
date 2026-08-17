import assert from "node:assert/strict";
import test from "node:test";
import type { ContainerApp, ProjectShare } from "../../models/project.ts";
import { AGENT_BROWSER_PORT } from "./projectShareState.ts";
import {
  hasPreviewPort,
  isPreviewLinkBusy,
  isPreviewLinkDone,
  issuedShareUrl,
  preferredPreviewPort,
  previewChipLabel,
  previewLinkError,
  previewLinkFeedbackInitial,
  previewLinkFeedbackReduce,
  previewPortRows,
  previewUnavailableReason,
} from "./projectPreviewLinksState.ts";

const now = Date.UTC(2026, 2, 1, 12, 0, 0);
const hour = 60 * 60 * 1000;

function share(overrides: Partial<ProjectShare> = {}): ProjectShare {
  return {
    id: "share-1",
    port: 3000,
    createdAt: now,
    expiresAt: now + 24 * hour,
    ...overrides,
  };
}

test("lists every listening port and marks platform ports unshareable", () => {
  const apps: ContainerApp[] = [
    { port: AGENT_BROWSER_PORT, process: "novnc" },
    { port: 9222, process: "chromium" },
    { port: 5173, process: "vite" },
    { port: 3000, process: "node" },
  ];

  const rows = previewPortRows(apps, [], now);

  assert.deepEqual(
    rows.map((row) => [row.port, row.shareable]),
    [
      [3000, true],
      [5173, true],
      [AGENT_BROWSER_PORT, false],
      [9222, false],
    ],
  );
});

test("collapses duplicate ports and keeps the first process label", () => {
  const apps: ContainerApp[] = [
    { port: 3000, process: "node" },
    { port: 3000, process: "node-ipv6" },
  ];

  const rows = previewPortRows(apps, [], now);

  assert.equal(rows.length, 1);
  assert.equal(rows[0].process, "node");
});

test("counts only live links, and only for ports still listening", () => {
  const apps: ContainerApp[] = [{ port: 3000 }, { port: 5173 }];
  const shares = [
    share({ id: "a", port: 3000 }),
    share({ id: "b", port: 3000, expiresAt: now - hour }),
    share({ id: "c", port: 8080 }),
  ];

  const rows = previewPortRows(apps, shares, now);

  assert.deepEqual(
    rows.map((row) => [row.port, row.shareCount]),
    [
      [3000, 1],
      [5173, 0],
    ],
  );
});

test("prefers a port that already has a share over the lowest port", () => {
  const apps: ContainerApp[] = [{ port: 3000 }, { port: 5173 }, { port: 8080 }];
  const rows = previewPortRows(apps, [share({ port: 5173 })], now);

  assert.equal(preferredPreviewPort(rows), 5173);
});

test("falls back to the lowest shareable port, ignoring platform ports", () => {
  const apps: ContainerApp[] = [
    { port: AGENT_BROWSER_PORT },
    { port: 8842 },
    { port: 5173 },
    { port: 3000 },
  ];
  const rows = previewPortRows(apps, [], now);

  assert.equal(preferredPreviewPort(rows), 3000);
  assert.equal(hasPreviewPort(rows), true);
});

test("offers no chip when only platform ports are listening", () => {
  const rows = previewPortRows([{ port: AGENT_BROWSER_PORT }, { port: 8081 }], [], now);

  assert.equal(preferredPreviewPort(rows), null);
  assert.equal(hasPreviewPort(rows), false);
  assert.equal(rows.length, 2);
});

test("picks the lowest shared port when several ports are shared", () => {
  const apps: ContainerApp[] = [{ port: 3000 }, { port: 5173 }, { port: 8080 }];
  const rows = previewPortRows(
    apps,
    [share({ id: "a", port: 8080 }), share({ id: "b", port: 5173 })],
    now,
  );

  assert.equal(preferredPreviewPort(rows), 5173);
});

test("names the chip after its port", () => {
  assert.equal(previewChipLabel(3000), "Preview :3000");
});

test("explains why a non-running container has nothing to show", () => {
  assert.equal(previewUnavailableReason("stopped"), "stopped");
  assert.equal(previewUnavailableReason("provisioning"), "provisioning");
  assert.equal(previewUnavailableReason("missing"), "missing");
  assert.equal(previewUnavailableReason("running"), null);
  assert.equal(previewUnavailableReason(""), null);
});

test("share flow moves from working to a copied link", () => {
  const started = previewLinkFeedbackReduce(previewLinkFeedbackInitial, {
    type: "start",
    action: "share",
    port: 3000,
  });
  assert.equal(isPreviewLinkBusy(started, "share", 3000), true);
  assert.equal(isPreviewLinkBusy(started, "share", 5173), false);
  assert.equal(isPreviewLinkBusy(started, "copy", 3000), false);

  const done = previewLinkFeedbackReduce(started, {
    type: "done",
    action: "share",
    port: 3000,
    url: "https://demo--3000.dev.example.com/?share=tok",
    copied: true,
  });

  assert.equal(isPreviewLinkDone(done, "share", 3000), true);
  assert.equal(issuedShareUrl(done), "https://demo--3000.dev.example.com/?share=tok");
  assert.equal(done.copied, true);
});

test("keeps the share URL when the clipboard write was refused", () => {
  const started = previewLinkFeedbackReduce(previewLinkFeedbackInitial, {
    type: "start",
    action: "share",
    port: 3000,
  });
  const done = previewLinkFeedbackReduce(started, {
    type: "done",
    action: "share",
    port: 3000,
    url: "https://demo--3000.dev.example.com/?share=tok",
    copied: false,
  });

  assert.equal(issuedShareUrl(done), "https://demo--3000.dev.example.com/?share=tok");
  assert.equal(done.copied, false);
});

test("drops a result that does not belong to the request in flight", () => {
  const started = previewLinkFeedbackReduce(previewLinkFeedbackInitial, {
    type: "start",
    action: "share",
    port: 5173,
  });

  const stale = previewLinkFeedbackReduce(started, {
    type: "done",
    action: "share",
    port: 3000,
    url: "https://demo--3000.dev.example.com/?share=old",
    copied: true,
  });
  assert.deepEqual(stale, started);

  const wrongAction = previewLinkFeedbackReduce(started, {
    type: "done",
    action: "copy",
    port: 5173,
    url: "https://demo--5173.dev.example.com",
    copied: true,
  });
  assert.deepEqual(wrongAction, started);

  const unsolicited = previewLinkFeedbackReduce(previewLinkFeedbackInitial, {
    type: "done",
    action: "share",
    port: 5173,
    url: "https://demo--5173.dev.example.com/?share=tok",
    copied: true,
  });
  assert.deepEqual(unsolicited, previewLinkFeedbackInitial);
});

test("reports a failure against the port that failed, then clears", () => {
  const started = previewLinkFeedbackReduce(previewLinkFeedbackInitial, {
    type: "start",
    action: "share",
    port: 3000,
  });
  const failed = previewLinkFeedbackReduce(started, {
    type: "failed",
    action: "share",
    port: 3000,
    error: "container is not running",
  });

  assert.equal(previewLinkError(failed, 3000), "container is not running");
  assert.equal(previewLinkError(failed, 5173), undefined);
  assert.equal(issuedShareUrl(failed), undefined);
  assert.deepEqual(
    previewLinkFeedbackReduce(failed, { type: "clear" }),
    previewLinkFeedbackInitial,
  );
});

test("a second request replaces the feedback of the first", () => {
  const first = previewLinkFeedbackReduce(previewLinkFeedbackInitial, {
    type: "start",
    action: "copy",
    port: 3000,
  });
  const copied = previewLinkFeedbackReduce(first, {
    type: "done",
    action: "copy",
    port: 3000,
    url: "https://demo--3000.dev.example.com",
    copied: true,
  });
  const second = previewLinkFeedbackReduce(copied, {
    type: "start",
    action: "share",
    port: 5173,
  });

  assert.equal(isPreviewLinkDone(second, "copy", 3000), false);
  assert.equal(isPreviewLinkBusy(second, "share", 5173), true);
});
