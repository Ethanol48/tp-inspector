# tpi — documentation

Full reference of `tpi`. For a quick start (compile, log in, run), see the
[README](README.md).

- [Building](#building)
- [Authentication](#authentication)
- [Usage](#usage)
- [Tabs](#tabs)
- [Keys](#keys)
- [Scoring](#scoring)
- [Repos](#repos)
- [Cache](#cache)
- [Cleaning up](#cleaning-up)
- [Operator API notes](#operator-api-notes)
- [Layout](#layout)

## Building

### Requirements

- **Go 1.26 or newer** (`go.mod` says `go 1.26.6`).
- `git` and `ssh` with your Forge key, only to clone repos from the Repos tab.
- Network access to `cri.epita.fr` and `operator.forge.epita.fr` at run time.

### Compile

```sh
go build -o tpi ./cmd/tpi
./tpi prog-205-p-01-2030
```

`go build` reads `go.mod` / `go.sum`, downloads the libraries (Bubble Tea,
Lip Gloss…) into `~/go/pkg/mod` and writes the `tpi` binary in the current
directory. Rebuild with the same command after changing the code.

### Install it in your PATH

```sh
go install ./cmd/tpi
```

Make sure `~/go/bin` (or `$(go env GOPATH)/bin`) is in your `PATH`, then run
`tpi` from anywhere. Alternatively copy the binary yourself:
`cp tpi ~/.local/bin/`.

### Check the code

```sh
go vet ./...                 # static checks
go test ./...                # unit tests (slug, trace parser, scoring, cleanup)
gofmt -l .                   # lists badly formatted files (gofmt -w . fixes them)
```

## Authentication

`tpi` implements the same OAuth2 PKCE login as `poulpy auth` (same CRI client
id, callback on `localhost:2097`) and shares the token file `~/.cri-tokens`, so
a session opened with either tool works for both. Expired tokens are
refreshed automatically; when the refresh token is also expired, `tpi` prints
a URL and opens the browser before starting the UI.

```sh
tpi login        # log in explicitly (optional)
```

## Usage

```
tpi [flags] <slug>
echo <slug> | tpi [flags]
tpi login
tpi --logins logins.txt prog-205-p-01-2030
tpi clean [--all | --repos] [-y]
tpi dump <path> [key=value...]
```

Flags must come **before** the slug (Go flag parsing stops at the first
positional argument).

| Flag | Default | Description |
|------|---------|-------------|
| `--tp SLUG` | | Assignment slug, alternative to the positional argument or stdin. |
| `--tenant SLUG` | looked up | Tenant of the activity (e.g. `epita-prepa-computer-science`). By default it is found in the list of activities you can access. |
| `--logins FILE` | | Text file of logins: the Overview and Ranking tabs show only them (toggle with `f` in each tab). It does **not** select repos to clone. |
| `--refresh` | off | Ignore cached API responses (same as pressing `R`). |
| `--cache DIR` | `~/.cache/tpi` | Cache directory. |

### Assignment slug

`prog-205-p-01-2030` reads as: subject `prog`, year `2`, bimester `05`, kind
`p` (TP; `e` exam, `r` retake…), number `01`, promo of `2030`.

### Logins file

One login per line. Blank lines and `# comments` are ignored; e-mail addresses
(`first.last@epita.fr`) and full group slugs are accepted.

```
# suspects prog-205
prenom.nom
another-name.surname
example.another@epita.fr
```

Logins that are not part of the assignment are reported in the status bar.

### `tpi dump`

Raw authenticated GET on the operator API, pretty-printed JSON on stdout —
handy to inspect endpoints. Path parameters containing `/` must be
URL-encoded:

```sh
tpi dump /api/activities/activity/epita-prepa-computer-science%2Fprog-205-p-01-2030
tpi dump /api/traces/epita-prepa-computer-science%2Fprog-205-p-01-2030/explore pageNum=0 pageSize=10
```

## Tabs

| Tab | Content |
|-----|---------|
| **Overview** | Students, submitted / missing, mean and median grade, grade histogram, submissions per day, summary of suspicious pairs. With `--logins`, it starts on the selection (list + marked students), with the promo mean and median next to theirs; `f` switches to the whole promo. |
| **Ranking** | Every student with grade, last tag, submission time, number of submissions, status and best suspicion scores. Sortable and filterable. |
| **Tests** | Pass rate per test, without the per-exercise global checks. `enter` groups the failing students by **exact failure output**: a rare wrong output shared by 2–5 students stands out. |
| **Traces** | Pairs of students with similar failure patterns (see scoring). |
| **Repos** | Clones the repos of the students **marked** in Ranking (only them), at the tag of their graded submit, and shows tag, commit and local path. |

`enter` on a student opens three sections (`tab` to switch):

- **Trace** — the trace of the graded `submit` submission, as before.
- **Tags** — every tag they pushed (`submit-*` and `archi-*`, newest first)
  with definition, date, status, grade, author and IP; ★ marks the graded
  submission of each definition. `enter` on a **submit** tag opens its trace;
  it is downloaded on demand, one at a time, only when not cached yet.
  `archi` tags are listed but not browsable (they only check the repository
  layout). In a tag trace, `[` / `]` move to the older / newer submit tag.
- **Suspects** — suspicious pairs involving the student.

A trace shows the grade, `passed/total` tests, and a tree Architecture ›
Build › Testsuite › category › exercise › function › test with ✓/✗ and `passed / total` on every node. Failing groups
are expanded down to the category level. The **Failures** panel on the right
(below on narrow terminals) shows the output of the selected test and how
many students fail it, or every failed test under the selected group.

`H` on a student (in Ranking or in their detail view) opens their
**history**: the grade of every activity (TPs, exams and retakes, `_d`
variants included) of their promo and of the previous and next ones, e.g.
every `*-2029`, `*-2030` and `*-2031` activity for a TP of 2030. The
neighbouring promos are what reveal a **repeated year**: a student who
repeated year 1 did it once with the 2029 promo and again with the 2030
promo.

The view has one tab per **academic year + promo** in which the student
submitted something (e.g. `Y1 · 2029 2024–25`, `Y1 · 2030 2025–26`,
`Y2 · 2030 2026–27`), plus a **Summary** tab: average cards over every year
and one row per year + promo with the TP / exam / retake / overall averages
and a warning when the same year was done several times. A year tab shows the
cards and the activities of that year only.

It asks for confirmation first (about 130 requests for a student of 2030),
then downloads one filtered request per activity (only grades, no traces) and
caches it in `~/.cache/tpi/students/<login>-<promo>.json`; opening it again
reads the cache, `U` downloads it again. The grade of an activity is the
newest submission of its graded definition (`submit` for TPs, `submission`
for exams); averages ignore activities without any submission.

From the Traces tab, `enter` opens a comparison view A vs B (failed tests
side by side with their outputs, shared IPs) and a **History** section: A's
tags then B's, each newest first, separated by a blank line, with the time to
the closest submission of the other student (red under 10 minutes).

## Keys

The footer shows the most relevant keys for the current view and drops the
rest when the terminal is narrow; `?` always lists everything.

| Key | Action |
|-----|--------|
| `←/→`, `tab`, `1`–`5` | Switch tab (or section inside a detail view) |
| `↑/↓`, `j/k`, `pgup/pgdn`, `g/G` | Move |
| `enter` | Open details / comparison |
| `esc` | Back |
| `q` | Back / quit |
| `m` | Switch submission definition (`submit` ↔ `archi`) |
| `R` | Reload from the API, ignoring the cache |
| `e` | Export suspicious pairs of the current definition to `tpi-<slug>-<def>-suspects.csv` in the current directory |
| `?` | Open / close the help |
| **Overview** | |
| `f` | Statistics of the `--logins` list + marked students / of the whole promo |
| **Ranking** | |
| `s` / `r` | Cycle sort (grade, submission time, login, submissions, suspicion) / reverse |
| `/` | Filter by login |
| `f` | Show only the `--logins` list + marked students |
| `space` | Mark a student (marked students are the ones cloned in Repos) |
| `c` | Compare the 2 marked students (or 1 marked + the cursor) |
| `H` | History of the student under the cursor: grades in every TP / exam by academic year + promo (asks before downloading) |
| `v` | Show / hide columns (IP is hidden by default); saved in `~/.config/tpi/config.json` |
| **Repos** | |
| `a` | Clone / update the marked repos |
| `u` | Unmark the student under the cursor |
| `r` | Remove the repo under the cursor (confirm with `y`) |
| **History** | |
| `tab`, `←/→` | Switch between Summary and the academic year + promo tabs |
| `enter` | In Summary: open that year |
| `s` / `r` | Cycle sort (activity, date, grade) / reverse |
| `k` | Show every kind / only TPs / exams / retakes |
| `a` | Also show activities without any submission |
| `U` | Download the history again (asks first) |
| `enter` | In a year, on the open TP: its student detail |
| **Student** | |
| `H` | History of the student |
| `enter` | On a submit tag: open its trace |
| `[` / `]` | In a tag trace: older / newer submit tag |
| `enter`, `space`, `←/→` | Fold / unfold a group (`←` on a test goes to its parent) |
| `n` / `N` | Next / previous failed test, unfolding its ancestors |
| `F` | Show failures only |
| `E` / `C` | Expand / collapse everything |

## Scoring

Scores are **hints**, not proof: always read the code before concluding.

### Traces

For each student, only *informative* failures are kept:

- build and architecture checks, `Error` tests and `Memory Error` counters
  are dropped;
- failures against the testsuite stub (`/stub/` in the output: file not
  submitted) are dropped;
- exercises failed at ≥ 90 % are dropped (not attempted);
- per-exercise failure patterns (exact set of failed tests and outputs)
  shared by more than max(4, 1 % of the promo) students are dropped: those
  are common mistakes such as an empty function returning `NULL`.


## Repos

Only the students marked with `space` in Ranking are cloned. Each repo is cloned 
(or fetched when the tag changed) into `~/.cache/tpi/<slug>/repos/<login>` and
checked out at the tag of the graded `submit` submission, 4 at a time over
SSH. The tab shows the tag, the checked-out commit and the local path; a repo
is `outdated` when a newer submit has been graded since it was cloned.
Repos already on disk from earlier runs are listed too, even when not marked,
so `r` can remove them (after a `y` confirmation).

## Cache

`~/.cache/tpi/`:

- `activities.json` — your activities;
- `<slug>/groups.json`, `sds.json`, `submissions.json` — API responses,
  reused until `R` / `--refresh`;
- `<slug>/traces/<job-id>.xml` — traces of graded and opened tags, immutable and always reused;
- `<slug>/repos/<login>` — clones of the marked students;
- `students/<login>-<promo>.json` — student histories downloaded with `H`.

The last submission per group is always fetched fresh (its trace URLs are
short-lived presigned links).

## Cleaning up

```
tpi clean              # interface: pick what to delete
tpi clean --repos      # delete every cloned repo, keep TP data
tpi clean --all        # delete the whole cache (TP data and repos)
```

`tpi clean` lists every cached assignment with its two kinds of data and
their size:

- **TP data** — cached API responses and traces; they are downloaded again
  the next time the assignment is opened;
- **repos** — the cloned repositories.

`space` selects an item, `a` selects everything, `r` every repos item, `n`
nothing, `d` deletes the selection after a `y` confirmation, `q` quits.

`--repos` and `--all` ask `[y/N]` on the terminal first (the default is no);
add `-y` to skip the question in scripts. Your CRI login (`~/.cri-tokens`)
and settings (`~/.config/tpi`) are never touched, and nothing outside the
cache directory can be deleted. `--cache DIR` works with `clean` too:
`tpi --cache DIR clean`.

## Operator API notes

Quirks found while building this, not in the Swagger spec:

- `GET /api/traces/{activityUri}/explore` returns **500** unless `pageNum` is
  sent explicitly, and **409** above `pageSize=100`.
- Path parameters containing `/` (activity and submission definition URIs)
  must be encoded as `%2F`.
- Student submission definitions are
  `<tenant>/<slug>/root/<slug>/{archi,submit}`; the `Assistants-*` and
  `dummy-submission` ones are ignored.

## Layout

```
cmd/tpi/                 CLI entry point
internal/tp/             slug parsing
internal/forge/          OAuth login and operator API client
internal/data/           dataset loading and disk cache
internal/trace/          JUnit trace parsing
internal/analysis/       trace similarity
internal/repos/          clone / fetch / checkout at a tag
internal/ui/             Bubble Tea interface
```
