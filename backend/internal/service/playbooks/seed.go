package playbooks

// Seed returns the library a fresh install starts with. It is written once,
// on the first start that finds no `playbooks.json`; from then on the document
// belongs to the operator and is never rewritten from here.
//
// Prompts are English on purpose: every provider (Claude, Codex, Kimi,
// Antigravity) behaves most predictably with English instructions, and the
// visible title carries the localization.
//
// Skill references point at the operator's global skill library by command
// name. A playbook that names a skill the server has not published still
// applies — the composer simply preselects a reference the agent will not find
// — so the admin Playbooks page flags unknown commands rather than the API
// refusing them.
func Seed() []Playbook {
	return Normalize([]Playbook{
		{
			ID:    "security-review",
			Title: "🔒 Security review",
			Icon:  "🔒",
			Hint:  "Read-only audit of the workspace with the guard skills.",
			Mode:  "review",
			Prompt: "Review the code in /workspace for security and correctness problems. " +
				"Apply the review protocol and the wp-guard, woo-guard, and clean-code-guard checklists " +
				"to every file that changed on the current branch, and to the entry points they reach.\n\n" +
				"Rules:\n" +
				"- Do NOT change, create, or delete any file. This is a read-only review.\n" +
				"- Report findings grouped by severity (blocker, major, minor), each with file path, line, " +
				"the concrete risk, and the smallest fix you would make.\n" +
				"- Call out escaping/sanitization, capability and nonce checks, SQL construction, secrets in " +
				"source, and unvalidated input explicitly.\n" +
				"- End with a short verdict: safe to ship, or the blocking list.",
			Skills: []SkillRef{
				{Name: "review-protocol", Command: "review-protocol", Source: "global"},
				{Name: "wp-guard", Command: "wp-guard", Source: "global"},
				{Name: "woo-guard", Command: "woo-guard", Source: "global"},
				{Name: "clean-code-guard", Command: "clean-code-guard", Source: "global"},
			},
			Order: 0,
		},
		{
			ID:    "update-and-test",
			Title: "⬆️ Update & test",
			Icon:  "⬆️",
			Hint:  "Update WordPress core, plugins, and themes on the staging copy, then smoke test.",
			Mode:  "code",
			Prompt: "Update the WordPress installation in /workspace and prove it still works.\n\n" +
				"Steps:\n" +
				"1. Confirm you are on the STAGING copy in /workspace, never a production site. Stop and ask " +
				"if you cannot confirm it.\n" +
				"2. Record the current versions (`wp core version`, `wp plugin list`, `wp theme list`).\n" +
				"3. Update core, then plugins, then themes, one group at a time.\n" +
				"4. Run the smoke tests: if a Playwright suite exists, run it; otherwise load the home page, " +
				"one inner page, and the admin dashboard and check for PHP errors or fatals in the logs.\n" +
				"5. Report a table of before/after versions, what broke, and what you did about it. " +
				"Do not deploy anything.",
			Skills: []SkillRef{
				{Name: "playwright-e2e", Command: "playwright-e2e", Source: "global"},
				{Name: "wp-guard", Command: "wp-guard", Source: "global"},
			},
			Order: 1,
		},
		{
			ID:    "prepare-for-delivery",
			Title: "🚚 Prepare for delivery",
			Icon:  "🚚",
			Hint:  "Tests, CHANGELOG from git log, tag, and a bilingual client summary.",
			Mode:  "code",
			Prompt: "Prepare this project for delivery to the client.\n\n" +
				"Steps:\n" +
				"1. Run the full test suite and report the result. Stop if it fails.\n" +
				"2. Read `git log` since the last tag and write a CHANGELOG entry for the new version " +
				"(Keep a Changelog format: Added / Changed / Fixed / Removed). Update CHANGELOG.md.\n" +
				"3. Create an annotated git tag for the new version. Do not push it.\n" +
				"4. Produce a client-facing summary of what changed, in Arabic and in English, written for a " +
				"non-technical reader — what is new, what was fixed, and anything they must do on their side.",
			Skills: []SkillRef{
				{Name: "docs-guard", Command: "docs-guard", Source: "global"},
				{Name: "test-guard", Command: "test-guard", Source: "global"},
			},
			Order: 2,
		},
		{
			ID:    "import-client-site",
			Title: "📥 Import client site from Hestia",
			Icon:  "📥",
			Hint:  "Pull a live Hestia site into /workspace as a staging copy.",
			Mode:  "code",
			Prompt: "Import the client site from the Hestia server into /workspace using the client-site-import " +
				"skill.\n\n" +
				"Rules:\n" +
				"- Read the HESTIA_HOST, HESTIA_USER, and HESTIA_PASSWORD (or SSH key) values from this " +
				"project's secrets. If any of them is missing, stop and tell me exactly which secret to add " +
				"in Project settings → Secrets. Never guess or invent credentials.\n" +
				"- Import files and database into a staging copy. Rewrite site URLs to the preview host and " +
				"disable outbound mail so the client is never contacted from staging.\n" +
				"- Finish by starting the site locally and reporting the preview port.",
			Skills: []SkillRef{
				{Name: "client-site-import", Command: "client-site-import", Source: "global"},
			},
			Order: 3,
		},
		{
			ID:    "deploy-to-hestia",
			Title: "🚀 Deploy to Hestia",
			Icon:  "🚀",
			Hint:  "Dry-run first, then deploy /workspace to the Hestia target.",
			Mode:  "code",
			Prompt: "Deploy {{project}} from /workspace to its Hestia target using the deploy-to-hestia skill.\n\n" +
				"Rules:\n" +
				"- Run a DRY RUN first and show me exactly what would change: files added, changed, deleted, " +
				"and any database migration.\n" +
				"- Stop after the dry run and wait for my explicit approval before touching the server.\n" +
				"- Read the HESTIA_* credentials from this project's secrets; if one is missing, say which.\n" +
				"- After an approved deploy, verify the live site responds and report the result.",
			Skills: []SkillRef{
				{Name: "deploy-to-hestia", Command: "deploy-to-hestia", Source: "global"},
			},
			Order: 4,
		},
		{
			ID:    "audit-live-site",
			Title: "🔎 Audit live site",
			Icon:  "🔎",
			Hint:  "Run the weekly site audit against a URL you provide.",
			Mode:  "chat",
			Prompt: "Run the weekly-site-audit skill against {{askUrl}}.\n\n" +
				"Cover performance, SEO, accessibility, broken links, and anything that looks broken or " +
				"outdated. Do not change any file in the workspace. Report the findings ordered by impact, " +
				"each with the evidence you saw and the concrete fix.",
			Skills: []SkillRef{
				{Name: "weekly-site-audit", Command: "weekly-site-audit", Source: "global"},
			},
			Order: 5,
		},
		{
			ID:    "write-e2e-tests",
			Title: "🧪 Write e2e tests",
			Icon:  "🧪",
			Hint:  "Playwright coverage for the main journeys of the app in /workspace.",
			Mode:  "code",
			Prompt: "Write Playwright end-to-end tests for the main user journeys of the app in /workspace, " +
				"using the playwright-e2e skill.\n\n" +
				"Steps:\n" +
				"1. Read the app and list the journeys that matter (sign-in, the primary create/read/update " +
				"path, checkout or submission if there is one). Show me the list before writing tests.\n" +
				"2. Set up Playwright if it is not already configured.\n" +
				"3. Write one spec per journey. Assert on user-visible outcomes, not on implementation " +
				"details, and keep each spec independent.\n" +
				"4. Run the suite against the app's preview URL {{previewUrl}} and report pass/fail per spec.",
			Skills: []SkillRef{
				{Name: "playwright-e2e", Command: "playwright-e2e", Source: "global"},
			},
			Order: 6,
		},
	})
}
