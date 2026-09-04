import assert from "node:assert/strict";
import test from "node:test";
import { deployHeadline, deployTargetLabel, type DeployTarget } from "../../models/deploy.ts";

function target(over: Partial<DeployTarget> = {}): DeployTarget {
  return {
    key: "SSH_LIVE",
    name: "live",
    host: "203.0.113.9",
    user: "deploy",
    port: 22,
    enabled: false,
    ...over,
  };
}

test("a target is named by its site, because nobody thinks in IPs", () => {
  assert.equal(deployTargetLabel(target({ domain: "client.example" })), "client.example");
  assert.equal(deployTargetLabel(target()), "203.0.113.9");
});

test("off is stated plainly, because it is the safe state and the common one", () => {
  assert.equal(deployHeadline([target(), target({ key: "B" })]), "Deploy off");
});

test("one live server is named, since that is the thing worth knowing", () => {
  // Before asking an agent to change anything, the question is "where does
  // this land?" — a count would not answer it.
  const headline = deployHeadline([
    target({ enabled: true, domain: "client.example" }),
    target({ key: "B" }),
  ]);
  assert.equal(headline, "Deploy → client.example");
});

test("several live servers become a count, and the list says which", () => {
  const headline = deployHeadline([
    target({ enabled: true, domain: "a.example" }),
    target({ key: "B", enabled: true, domain: "b.example" }),
  ]);
  assert.equal(headline, "Deploy → 2 servers");
});
