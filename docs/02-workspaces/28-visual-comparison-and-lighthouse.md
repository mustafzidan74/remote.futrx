# Visual Comparison and Lighthouse

Two ways to check work that the platform could not check before, both running a
headless browser inside the project's own container against its own loopback
preview. Neither needs a published site, an API key, or a network round trip.

They live on the project page as the **Visual** and **Lighthouse** tabs.

---

## Visual: before and after

### The problem

An agent edits a stylesheet and reports success. The edit did what it said, the
page it was aimed at looks right, and three pages away a footer has collapsed.
Nothing in the platform notices, because nothing was looking: the tests pass,
the container is healthy, and the preview returns 200. The damage is found by a
client.

### How it works

1. **Take a baseline** before the work. Pick a preview port, list up to twelve
   page paths, and the platform photographs each one at a fixed viewport.
2. **Do the work.**
3. **Compare now.** The same pages are photographed again and diffed against
   the baseline.

Each page reports how far it moved, worst first, with three views: the
**before**, the **after**, and a **diff** overlay — the current page faded out
with every changed pixel painted red, so one image answers both "what changed"
and "where on the page".

The point is not the page you edited. You knew that would change. It is the
page you never touched showing up at 4%.

### One baseline per project

A project has exactly one baseline at a time, and taking a new one discards the
comparisons made against the old one.

That is deliberate. A baseline is a commitment — "this is what correct looks
like" — and a shelf of them invites comparing against the wrong one, which
produces numbers that look authoritative and mean nothing. Keeping old
comparisons after a re-baseline would be worse still: they measure against
pictures that are no longer what correct means, and nothing on screen would say
so.

Re-baselining is how you accept a change. It is a decision, so it is a button
and not a side effect of running a comparison.

### Reading the numbers

| Reading | What it means |
|---|---|
| **unchanged** | Under 0.1% of pixels moved. |
| **a percentage** | That share of the compared area differs. |
| **page height changed** | The page's own dimensions moved. |
| **could not compare** | The page failed to load, or its baseline image is gone. |

Two things are worth knowing about how the number is produced:

**Pixels are compared perceptually, not numerically.** Plain RGB distance rates
a shift in blue as loudly as the same shift in green, while an eye barely
notices the first and cannot miss the second. Comparing in YIQ — weighting by
luminance — is what keeps antialiased text quiet and a moved element loud. A
page nobody touched reports exactly 0.00%, which is the property the whole
feature rests on: a tool that reports 0.4% for an untouched page teaches you to
stop reading the number.

**A page that grew is compared over the union, not the overlap.** Everything
past the old last row exists in one image and not the other, so it counts as
changed, and the size change is flagged separately. A modest percentage on a
page that grew 200px taller would read as a modest change, and it is not.

### What will show up as changed and should not

Anything that legitimately differs between two page loads: a rotating carousel,
a "posted 3 minutes ago" timestamp, a randomised testimonial, an animation
caught mid-frame. These are real differences, and the platform has no way to
know you did not mean them.

If a page is dominated by that kind of content, either leave it out of the
baseline or read its number as noise and look at the diff image instead.

### Limits

- Up to **12 pages** per baseline; each page is a browser launch, so twelve is
  a few minutes of container work.
- The last **10 comparisons** are kept; older ones and their images are
  evicted.
- Both the baseline and the comparison run in the background and fill in page
  by page, so the panel shows progress rather than a spinner.
- One run at a time per project. A second run would photograph a container
  already under a browser and report the interference as a change.

---

## Lighthouse: local audits

### Why not the PageSpeed API

The PageSpeed Insights API needs a key, is rate limited, only reaches pages
that are already published, and audits the single URL you hand it.

A site being rebuilt is none of those things. It is unpublished, it is behind a
login, it has twenty templates rather than one, and it gets measured twenty
times a day rather than once a week.

The container already has the headless Chromium that Playwright installed, so
running the real Lighthouse locally answers the same numbers with none of that.

### How it works

Pick a preview port, list up to six paths, choose **mobile** or **desktop**,
and run it. Each page produces:

- the four **category scores** — Performance, Accessibility, Best Practices,
  SEO — with the change since the last comparable run beside each one;
- the **Core Web Vitals** and the metrics that explain them: LCP, CLS, TBT,
  FCP, Speed Index, Server Response Time;
- the **failing audits worth acting on**, ordered by the time Lighthouse
  estimates fixing them would return.

Mobile is the default because it is the number Google ranks on. An operator who
only ever looks at desktop is measuring something their search traffic does not
experience.

### Reading the numbers

The score bands are **Lighthouse's own**: 90 and up is good, 50 and up is
average, below that is poor. They are copied rather than invented, so a colour
here means exactly what the same score means in Chrome's own report and in
PageSpeed Insights.

**A dash is not a zero.** Lighthouse omits a category it could not compute, and
the panel shows that as `—`. A missing score drawn as 0 in a red ring would say
the page failed completely when in fact nothing was measured.

**These are lab numbers.** One machine, simulated throttling — the same thing
PageSpeed's lab half reports. Real-user field data is a different measurement
and this never pretends otherwise.

**The trend only compares like with like.** A score is shown against the last
run of the *same page at the same form factor*. A mobile score next to a
desktop one is two different measurements, not a change.

### The install button

New base images ship with the Lighthouse CLI. A project created before that has
its own container filesystem and would never get it from a rebuild it is not
part of, so the Lighthouse tab offers a **one-off install** into that
container — about a minute, once, and the panel says so plainly rather than
letting six pages fail the same way.

To get it everywhere at once instead, rebuild the base image and run
`upgrade-workspaces`.

### Limits

- Up to **6 pages** per run; roughly half a minute each under throttling.
- The last **20 runs** are kept, which is deliberately more than a run holds:
  the point of keeping them is the trend, and a fortnight of weekly checks has
  to fit.
- One run at a time per project. Two Lighthouse runs in one container measure
  each other — the second one's numbers would include the first one's browser
  competing for the CPU, which is worse than no numbers because they look real.

### What is not stored

Lighthouse's own HTML report is never kept or served.

That report is a full HTML document carrying the audited page's titles, URLs
and screenshots. Serving it from the platform's origin would hand a hostile
page a script running in your session — the same reason the workspace file
preview is served from the container and not from the platform. The numbers are
parsed out of the JSON and rendered natively instead, which also turns 300 KB
per page into about 4 KB and is what makes keeping twenty runs affordable.

---

## Storage

| | |
|---|---|
| Visual images and index | `DATA_DIR/visual/<projectID>/` |
| Lighthouse history | `DATA_DIR/lighthouse/<projectID>.json` |

Both are mode 0700/0600, written atomically, and readable only through the
project's own session-gated routes — an image file that is not referenced by
the project's records is not served, so a guessed name reaches nothing.

## Permissions

Any project member can take a baseline, run a comparison, run an audit, and
install the CLI. Both features record to the audit log: `project.visualdiff`
and `project.lighthouse`. The install is recorded because it changes what is
inside a project's container, which is exactly the kind of thing you later want
to be able to find.
