import assert from "node:assert/strict";
import test from "node:test";
import {
  SCHEDULE_TEMPLATES,
  findScheduleTemplate,
  templateToCreateInput,
} from "./scheduleTemplates.ts";

test("every template is complete enough to submit", () => {
  assert.ok(SCHEDULE_TEMPLATES.length >= 4);
  const ids = new Set<string>();
  for (const template of SCHEDULE_TEMPLATES) {
    assert.ok(!ids.has(template.id), `duplicate template id ${template.id}`);
    ids.add(template.id);
    assert.ok(template.name.trim(), `${template.id} has no name`);
    assert.ok(template.blurb.trim(), `${template.id} has no blurb`);
    assert.ok(template.prompt.trim().length > 80, `${template.id} prompt is too thin`);
    assert.ok(template.skills.length > 0, `${template.id} names no skills`);
    // Five fields, no seconds — the backend rejects anything else.
    assert.equal(
      template.cron.trim().split(/\s+/).length,
      5,
      `${template.id} cron is not five fields`
    );
  }
});

test("every template prompt asks for a verdict marker", () => {
  for (const template of SCHEDULE_TEMPLATES) {
    assert.match(
      template.prompt,
      /<<RESULT: .*>>/,
      `${template.id} never teaches the verdict marker`
    );
  }
});

test("findScheduleTemplate resolves by id", () => {
  assert.equal(findScheduleTemplate("weekly-site-audit")?.cron, "0 7 * * 1");
  assert.equal(findScheduleTemplate("nope"), undefined);
});

test("templateToCreateInput pre-fills a cron task in the reader's timezone", () => {
  const template = findScheduleTemplate("nightly-backup-verify")!;
  const input = templateToCreateInput(template, "America/Toronto");

  assert.equal(input.kind, "cron");
  assert.equal(input.cron, template.cron);
  assert.equal(input.name, template.taskName);
  assert.equal(input.timezone, "America/Toronto");
  assert.deepEqual(input.condition, template.condition);
  // The copy must be independent: editing the form cannot mutate the library.
  input.condition!.command = "changed";
  assert.notEqual(template.condition!.command, "changed");
});

test("templateToCreateInput falls back to UTC", () => {
  const template = SCHEDULE_TEMPLATES[0];
  assert.equal(templateToCreateInput(template, "").timezone, "UTC");
});
