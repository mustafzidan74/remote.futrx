import assert from "node:assert/strict";
import test from "node:test";
import type { ProjectTemplate } from "../../models/template.ts";
import { newProjectState, type NewProjectState } from "./newProjectState.ts";

const catalog: ProjectTemplate[] = [
  {
    name: "blank",
    title: "Blank",
    description: "Nothing extra.",
    icon: "blank",
    default: true,
    provisions: false,
    prebuiltImageAvailable: false,
  },
  {
    name: "wordpress",
    title: "WordPress",
    description: "PHP + MariaDB + WP-CLI.",
    icon: "wordpress",
    defaultPorts: [8080],
    default: false,
    provisions: true,
    prebuiltImage: "futrx-remote-wordpress-base",
    prebuiltImageAvailable: false,
    inputs: [
      {
        key: "siteTitle",
        label: "Site title",
        type: "text",
        required: true,
        defaultFrom: "projectName",
      },
      {
        key: "adminEmail",
        label: "Admin email",
        type: "email",
        required: true,
        defaultFrom: "userEmail",
      },
      { key: "adminUser", label: "Admin username", type: "text", required: true, default: "admin" },
      {
        key: "adminPassword",
        label: "Admin password",
        type: "password",
        secret: true,
        secretName: "WP_ADMIN_PASSWORD",
        generate: true,
      },
      {
        key: "language",
        label: "Site language",
        type: "select",
        default: "ar",
        options: [
          { value: "ar", label: "العربية (RTL)" },
          { value: "en_US", label: "English (US)" },
        ],
      },
      { key: "installWoocommerce", label: "Install WooCommerce", type: "checkbox" },
    ],
  },
  {
    name: "laravel",
    title: "Laravel",
    description: "PHP + Composer + MariaDB.",
    icon: "laravel",
    default: false,
    provisions: true,
    prebuiltImage: "futrx-remote-laravel-base",
    prebuiltImageAvailable: true,
  },
];

function loaded(): NewProjectState {
  const opened = newProjectState.reduce(newProjectState.createInitial(), { type: "open" });
  return newProjectState.reduce(opened, { type: "templates-loaded", templates: catalog });
}

test("starts closed on the default template", () => {
  const initial = newProjectState.createInitial();
  assert.equal(initial.open, false);
  assert.equal(initial.template, "blank");
  assert.equal(newProjectState.canSubmit(initial), false);
});

test("loading the catalog keeps the catalog's own default", () => {
  const state = loaded();
  assert.equal(state.template, "blank");
  assert.equal(state.templates.length, 3);
  assert.equal(state.templatesLoading, false);
});

test("a selection that survives a catalog reload is kept", () => {
  const selected = newProjectState.reduce(loaded(), {
    type: "select-template",
    template: "wordpress",
  });
  const reloaded = newProjectState.reduce(selected, {
    type: "templates-loaded",
    templates: catalog,
  });
  assert.equal(reloaded.template, "wordpress");
});

test("a selection the catalog no longer offers falls back to the default", () => {
  const selected = newProjectState.reduce(loaded(), {
    type: "select-template",
    template: "wordpress",
  });
  const reloaded = newProjectState.reduce(selected, {
    type: "templates-loaded",
    templates: [catalog[0]],
  });
  assert.equal(reloaded.template, "blank");
});

test("a failed catalog still allows creating a default project", () => {
  const failed = newProjectState.reduce(loaded(), {
    type: "templates-failed",
    error: "503",
  });
  assert.equal(failed.template, "blank");
  assert.equal(failed.templatesError, "503");
  assert.equal(failed.templates.length, 0);

  const named = newProjectState.reduce(failed, { type: "set-name", name: "Site" });
  assert.equal(newProjectState.canSubmit(named), true);
});

test("reopening clears the form but keeps the loaded catalog", () => {
  const dirty = newProjectState.reduce(
    newProjectState.reduce(loaded(), { type: "set-name", name: "Draft" }),
    { type: "select-template", template: "laravel" }
  );
  const failed = newProjectState.reduce(dirty, { type: "submit-failed", error: "boom" });
  const reopened = newProjectState.reduce(failed, { type: "open" });

  assert.equal(reopened.name, "");
  assert.equal(reopened.error, "");
  assert.equal(reopened.submitting, false);
  assert.equal(reopened.template, "blank");
  assert.equal(reopened.templates.length, 3);
});

test("submission requires a non-blank name and blocks double submits", () => {
  const blank = newProjectState.reduce(loaded(), { type: "set-name", name: "   " });
  assert.equal(newProjectState.canSubmit(blank), false);

  const named = newProjectState.reduce(loaded(), { type: "set-name", name: "  Site  " });
  assert.equal(newProjectState.submittedName(named), "Site");
  assert.equal(newProjectState.canSubmit(named), true);

  const submitting = newProjectState.reduce(named, { type: "submit" });
  assert.equal(newProjectState.canSubmit(submitting), false);

  const failed = newProjectState.reduce(submitting, { type: "submit-failed", error: "409" });
  assert.equal(failed.error, "409");
  assert.equal(newProjectState.canSubmit(failed), true);
});

test("the first-start notice reflects whether a prebuilt image is published", () => {
  const blank = loaded();
  assert.equal(newProjectState.firstStartNotice(blank), "");

  const slow = newProjectState.reduce(blank, { type: "select-template", template: "wordpress" });
  assert.match(newProjectState.firstStartNotice(slow), /several minutes/);

  const fast = newProjectState.reduce(blank, { type: "select-template", template: "laravel" });
  assert.match(newProjectState.firstStartNotice(fast), /starts immediately/);
});

test("closing leaves no in-flight submission behind", () => {
  const submitting = newProjectState.reduce(
    newProjectState.reduce(loaded(), { type: "set-name", name: "Site" }),
    { type: "submit" }
  );
  const closed = newProjectState.reduce(submitting, { type: "close" });
  assert.equal(closed.open, false);
  assert.equal(closed.submitting, false);
});

function wordpress(name = "My Shop"): NewProjectState {
  const selected = newProjectState.reduce(loaded(), {
    type: "select-template",
    template: "wordpress",
  });
  return newProjectState.reduce(selected, { type: "set-name", name });
}

test("selecting a template prefills its declared defaults", () => {
  const state = wordpress();

  assert.equal(state.inputs.siteTitle, "My Shop");
  assert.equal(state.inputs.adminUser, "admin");
  assert.equal(state.inputs.language, "ar");
  assert.equal(state.inputs.installWoocommerce, "false");
  // The browser does not know the session's address; the server fills it in.
  assert.equal(state.inputs.adminEmail, "");
  assert.equal(newProjectState.canSubmit(state), true);
});

test("templates without inputs render no extra form", () => {
  const state = newProjectState.reduce(loaded(), { type: "set-name", name: "Plain" });

  assert.deepEqual(newProjectState.inputs(state), []);
  assert.deepEqual(newProjectState.submittedInputs(state), {});
});

test("the site title follows the project name until it is edited", () => {
  const renamed = newProjectState.reduce(wordpress(), { type: "set-name", name: "Metro" });
  assert.equal(renamed.inputs.siteTitle, "Metro");

  const edited = newProjectState.reduce(renamed, {
    type: "set-input",
    key: "siteTitle",
    value: "Metro Store",
  });
  const renamedAgain = newProjectState.reduce(edited, { type: "set-name", name: "Other" });
  assert.equal(renamedAgain.inputs.siteTitle, "Metro Store");
});

test("switching templates discards the previous template's answers", () => {
  const edited = newProjectState.reduce(wordpress(), {
    type: "set-input",
    key: "siteTitle",
    value: "Metro Store",
  });
  const switched = newProjectState.reduce(edited, {
    type: "select-template",
    template: "laravel",
  });
  assert.deepEqual(switched.inputs, {});

  const back = newProjectState.reduce(switched, {
    type: "select-template",
    template: "wordpress",
  });
  assert.equal(back.inputs.siteTitle, "My Shop");
  assert.deepEqual(back.touched, {});
});

test("input validation blocks submission and explains itself once edited", () => {
  const state = wordpress();
  const inputs = newProjectState.inputs(state);
  const email = inputs.find((input) => input.key === "adminEmail")!;
  const title = inputs.find((input) => input.key === "siteTitle")!;

  const badEmail = newProjectState.reduce(state, {
    type: "set-input",
    key: "adminEmail",
    value: "nope",
  });
  assert.equal(newProjectState.canSubmit(badEmail), false);
  assert.match(newProjectState.visibleInputError(badEmail, email), /email address/);

  // Blanking a field the server can fill (from the project name here) is not
  // an error: it is omitted from the request and the default applies.
  const blankTitle = newProjectState.reduce(state, {
    type: "set-input",
    key: "siteTitle",
    value: "  ",
  });
  assert.equal(newProjectState.visibleInputError(blankTitle, title), "");
  assert.equal(newProjectState.canSubmit(blankTitle), true);
  assert.equal("siteTitle" in newProjectState.submittedInputs(blankTitle), false);

  // An untouched field never shows an error, even when it has one.
  assert.equal(newProjectState.visibleInputError(state, email), "");

  const fixed = newProjectState.reduce(badEmail, {
    type: "set-input",
    key: "adminEmail",
    value: "owner@example.com",
  });
  assert.equal(newProjectState.visibleInputError(fixed, email), "");
  assert.equal(newProjectState.canSubmit(fixed), true);
});

test("an empty optional value is omitted so the server applies its own default", () => {
  const state = newProjectState.reduce(wordpress(), {
    type: "set-input",
    key: "installWoocommerce",
    value: "true",
  });

  assert.deepEqual(newProjectState.submittedInputs(state), {
    siteTitle: "My Shop",
    adminUser: "admin",
    language: "ar",
    installWoocommerce: "true",
  });
});

test("a password is submitted verbatim, spaces included", () => {
  const state = newProjectState.reduce(wordpress(), {
    type: "set-input",
    key: "adminPassword",
    value: " pa ss ",
  });

  assert.equal(newProjectState.submittedInputs(state).adminPassword, " pa ss ");
});

test("reopening clears the answers of the template it lands on", () => {
  const edited = newProjectState.reduce(wordpress(), {
    type: "set-input",
    key: "language",
    value: "en_US",
  });
  const reopened = newProjectState.reduce(edited, { type: "open" });

  assert.equal(reopened.template, "blank");
  assert.deepEqual(reopened.inputs, {});
  assert.deepEqual(reopened.touched, {});
});
