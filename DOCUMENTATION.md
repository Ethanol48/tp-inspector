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

| Flag            | Default        | Description                                                                                                                                                 |
| --------------- | -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--tp SLUG`     |                | Assignment slug, alternative to the positional argument or stdin.                                                                                           |
| `--tenant SLUG` | looked up      | Tenant of the activity (`epita-prepa-computer-science`). By default it is found in the list of activities you can access.                                   |
| `--logins FILE` |                | Text file of logins: your **group**. The Overview and Ranking start on them (toggle with `f` in each tab), the Group tab compares them with the promo, Code shows only their pairs, `A` in Repos clones them. |
| `--refresh`     | off            | Ignore cached API responses (same as pressing `R`).                                                                                                         |
| `--cache DIR`   | `~/.cache/tpi` | Cache directory.                                                                                                                                            |

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

The **group** is the logins of the file plus the students marked with `space`
in Ranking, so a group can also be built by hand without a file. Statistics,
risk and code similarity are always computed on the whole promo (code: every
repo on disk); the group only
selects what is shown and what it is compared with.

### `tpi dump`

Raw authenticated GET on the operator API, pretty-printed JSON on stdout —
handy to inspect endpoints. Path parameters containing `/` must be
URL-encoded:

```sh
tpi dump /api/activities/activity/epita-prepa-computer-science%2Fprog-205-p-01-2030
tpi dump /api/traces/epita-prepa-computer-science%2Fprog-205-p-01-2030/explore pageNum=0 pageSize=10
```

## Tabs

| Tab          | Content                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Overview** | Students, submitted / missing, mean and median grade, grade histogram, submissions per day, the submission window (opening → closing) and the number of risky students, code pairs and AI-looking repos per level. With `--logins`, it starts on the selection (list + marked students), with the promo mean and median next to theirs; `f` switches to the whole promo. |
| **Group**    | The group against the promo: students, submitted, no submission, errored, mean, median, at 100 %, average promo percentile (50 = average) and code pairs involving the group. `t` switches between **Members** (grade, promo percentile, status, suspicion level and flags: _no submission_, _errored_, _bottom 25 %_ of the promo, _suspect_; flagged members first) and **Exercises** (group vs promo pass rate per exercise, worst gap first).                                |
| **Ranking**  | Every student with grade, last tag, submission time, position in the window (`+3h` after the opening, `-12min` before the closing, in yellow when in the first 10 % / last 3 %), number of submissions, status and the suspicion level with its parts: risk, code, AI. Sorting by risk ranks everyone from the submissions alone (no clone needed); sorting by suspicion puts the analysed students first, then the others by risk. Only the group by default when there is one. |
| **Tests**    | Pass rate per test, without the per-exercise global checks; with a group, also the group pass rate and the gap with the promo (`s` sorts by gap). `enter` groups the failing students by **exact failure output**: a rare wrong output shared by 2–5 students stands out; the _In group_ column counts your students.                                                                                                                                                            |
| **Code**     | Pairs of repos sharing code: level, reason in words (number of functions alike, best match), score, AI level of each and grades. The panel below explains the selected pair. With a group, only pairs involving one of its members (◦) are shown; `f` shows everyone. Empty until repos are cloned.                                                                                                                                                                              |
| **Repos**    | Clones your group (`A`: the `--logins` list plus marked students, or the whole promotion without a list) or the students **marked** in Ranking (`a`), at the tag of their graded submission (`submit-*`, or `submission-*` for exams), and shows tag, commit, code and AI levels and local path. Every repo on disk is then analysed in the background.                                                                                                                           |

Students push three kinds of tags, one per submission definition:

| Tag            | Definition   | What it does                                                                                          |
| -------------- | ------------ | ----------------------------------------------------------------------------------------------------- |
| `archi-*`      | `archi`      | Only checks the repository structure (files and folders): no grade worth reading, no browsable trace. |
| `submit-*`     | `submit`     | TPs: runs the testsuite, produces the trace and the grade.                                            |
| `submission-*` | `submission` | Exams: the same as `submit`, for exams.                                                               |

The **graded definition** is `submit` for a TP and `submission` for an exam
(otherwise the first definition that is not `archi`): it is the one shown
first, whose traces are browsed, whose graded tag is cloned and timed by the
risk score. `m` switches to the other definitions.

`enter` on a student opens three sections (`tab` to switch):

- **Trace**: the trace of the graded submission (`submit` or `submission`).
- **Tags**: every tag they pushed (`submit-*` / `submission-*` and `archi-*`,
  newest first) with definition, date, status, grade, author and IP; ★ marks
  the graded submission of each definition. `enter` on a `submit-*` /
  `submission-*` tag opens its trace; it is downloaded on demand, one at a
  time, only when not cached yet. `archi-*` tags are listed but not
  browsable. In a tag trace, `[` / `]` move to the older / newer one.
- **Suspects**: their risk, AI score and the repos sharing code with theirs.

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

The **Suspects** section of a student lists their risk (with the reasons
and the window), their AI score (with each signal and the functions to read
first) and the repos sharing code with theirs; `enter` on a code pair opens
the comparison.

From the Code tab, `enter` opens a comparison view A vs B: the risk and AI
levels of both, shared IPs, the code pair's factors, a **Code** section
listing the matched functions with both bodies side by side, and a
**History** section: A's tags then B's, each newest first, separated by a blank line, with the time to the closest submission of the other student (red under 10 minutes).

## Keys

The footer shows the most relevant keys for the current view and drops the
rest when the terminal is narrow; `?` always lists everything.

| Key                              | Action                                                                                                                                                            |
| -------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `←/→`, `tab`, `1`–`6`            | Switch tab (or section inside a detail view)                                                                                                                      |
| `↑/↓`, `pgup/pgdn`, `g/G`        | Move                                                                                                                                                              |
| `enter`                          | Open details / comparison                                                                                                                                         |
| `esc`                            | Back                                                                                                                                                              |
| `q`                              | Back / quit                                                                                                                                                       |
| `m`                              | Switch submission definition (`submit` / `submission` ↔ `archi`)                                                                                                  |
| `R`                              | Reload from the API, ignoring the cache                                                                                                                           |
| `e`                              | Export the risk of every student, the code pairs and the AI scores to `tpi-<slug>-<def>-suspects.csv` in the current directory (`kind` is `risk`, `code` or `ai`) |
| `?`                              | Open / close the help (`↑/↓` scroll it)                                                                                                                           |
| **Overview**                     |                                                                                                                                                                   |
| `f`                              | Statistics of the `--logins` list + marked students / of the whole promo                                                                                          |
| **Group**                        |                                                                                                                                                                   |
| `t`                              | Switch between Members and Exercises                                                                                                                              |
| `enter` / `H`                    | On a member: their detail / history                                                                                                                               |
| **Tests**                        |                                                                                                                                                                   |
| `s`                              | Cycle sort: pass rate, name, group gap (with a group)                                                                                                             |
| **Code**                         |                                                                                                                                                                   |
| `f`                              | Only pairs with a member of the group / everyone                                                                                                                  |
| `enter`                          | Compare the pair                                                                                                                                                  |
| **Ranking**                      |                                                                                                                                                                   |
| `s` / `r`                        | Cycle sort (grade, risk, submission time, login, submissions, suspicion) / reverse                                                                                |
| `/`                              | Filter by login                                                                                                                                                   |
| `f`                              | Show only the `--logins` list + marked students                                                                                                                   |
| `space`                          | Mark a student (`a` in Repos clones the marked students)                                                                                                          |
| `c`                              | Compare the 2 marked students (or 1 marked + the cursor)                                                                                                          |
| `H`                              | History of the student under the cursor: grades in every TP / exam by academic year + promo (asks before downloading)                                             |
| `v`                              | Show / hide columns (IP is hidden by default; also risk, code, AI and window position); saved in `~/.config/tpi/config.json`                                      |
| **Repos**                        |                                                                                                                                                                   |
| `A`                              | Clone / update your group: `--logins` list + marked students, or the whole promotion without a list (confirm with `y`)                                             |
| `K`                              | Unlock your SSH key (`ssh-add`) after cloning was refused                                                                                                         |
| `a`                              | Clone / update the marked repos                                                                                                                                   |
| `u`                              | Unmark the student under the cursor                                                                                                                               |
| `r`                              | Remove the repo under the cursor (confirm with `y`)                                                                                                               |
| **History**                      |                                                                                                                                                                   |
| `tab`, `←/→`                     | Switch between Summary and the academic year + promo tabs                                                                                                         |
| `enter`                          | In Summary: open that year                                                                                                                                        |
| `s` / `r`                        | Cycle sort (activity, date, grade) / reverse                                                                                                                      |
| `k`                              | Show every kind / only TPs / exams / retakes                                                                                                                      |
| `a`                              | Also show activities without any submission                                                                                                                       |
| `U`                              | Download the history again (asks first)                                                                                                                           |
| `enter`                          | In a year, on the open TP: its student detail                                                                                                                     |
| **Student**                      |                                                                                                                                                                   |
| `H`                              | History of the student                                                                                                                                            |
| `space`                          | In Tags / Suspects: mark / unmark the student                                                                                                                     |
| `enter`                          | On a submit tag: open its trace; in Suspects, on a code pair: compare                                                                                             |
| `[` / `]`                        | In a tag trace: older / newer submit tag                                                                                                                          |
| `enter`, `space`, `←/→`          | Fold / unfold a group (`←` on a test goes to its parent)                                                                                                          |
| `n` / `N`                        | Next / previous failed test, unfolding its ancestors                                                                                                              |
| `F`                              | Show failures only                                                                                                                                                |
| `E` / `C`                        | Expand / collapse everything                                                                                                                                      |

## Scoring

Scores are **hints**, not proof: always read the code before concluding.

Suspicion combines three scores, each between 0 and 1, as independent
evidence: `suspicion = 1 − (1 − risk)(1 − code)(1 − AI)`. Each signal raises
it and several together more than any alone: risk 50 % and code 50 % give
75 %. Suspicion is only shown once the student's repo has been analysed
(`·` before, like Code and AI, and last when sorting by suspicion): without
the code it would only repeat the risk, which has its own column. Every score is shown as a level: **high** from 80 %,
**medium** from 50 %, **low** below. Failing the
same tests as someone else is **not** a signal: students who skip the same
exercises fail the same tests, while the students most likely to have copied
or generated their code pass everything.

### Risk

Computed for every student from the submissions alone, no clone needed.

| Factor     | Meaning                                                                                                                                                                                                                                                                                                                                                                          |
| ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Grade**  | `(grade / 100)²`: 100 % stands out, 60 % barely counts, a failing student is never risky.                                                                                                                                                                                                                                                                                        |
| **Timing** | Where the student _first reached their final grade_ falls in the submission window (`openingAt` → `closingAt` of `GET /api/activities/assignment/{assignmentUri}`). 1 at the opening, fading to 0 at 10 % of the window; 0 until the last 3 %, rising to 1 at the closing (and after it). Without a window from the API, the promo's first and last submissions stand in for it. |
| **Jump**   | 0.5 when the first submission is already ≥ 90 %; otherwise grows from 0 (+30 points) to 0.5 (+80 points) between two consecutive submissions reaching ≥ 80 %. Disabled on all-or-nothing definitions such as `archi` (≥ 90 % of the grades are 0 or 100 %).                                                                                                                      |

`risk = grade × (0.4 + 0.6 × min(1, timing + jump))`: 100 % built step by
step in the middle of the window gives 40 % (low), 100 % at the first try
70 % (medium), 100 % in the first hours or the last minutes up to 100 %
(high), especially at the first try. The thresholds are constants in
`internal/analysis/risk.go`.

### Code

Every repo on disk (C `.c`/`.h` and Python `.py` files, up to 200 KB, build and
hidden directories skipped) is split into functions, and only their
**bodies** are compared: the subject gives every student the same function
names and parameters, so a signature proves nothing (the name is still shown).
Tokens are normalized:
identifiers become `I`, numbers `N`, strings `S`, comments are dropped, so
renaming variables and reformatting does not hide a copy. Each function is
fingerprinted with winnowing (as in MOSS: hashes of 12-token k-grams, the
smallest of every 8).

- A fingerprint found in more than 20 % of the repos (at least 3, at most 30)
  is the subject's skeleton or code everybody writes, and is ignored.
  Bodies under 20 tokens (a getter, `return a + b;`: there is only one way to
  write them) or left with fewer than 4 specific fingerprints are ignored.
- Everything is done twice: on normalized tokens (sees through renaming) and
  on the tokens **as written** (only whitespace, tabs, line breaks and
  comments ignored: what diffing two files formatted with the same
  `.clang-format` would show). Students who write the same algorithm look
  alike once normalized, so its common shapes are dropped there; a version
  shared word for word by a few repos is still caught by the exact layer.
  Whole bodies identical as written (≥ 20 tokens, in few repos) always
  match.
- Two functions are **alike** when they share ≥ 50 % of the specific
  fingerprints of the smaller one, at least 4 of them, covering ≥ 25 % of all
  its fingerprints (a few specific ones in common code match by chance);
  functions are matched one to one, best first.

| Factor         | Meaning                                                                                                                                                 |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Similarity** | Token-weighted mean similarity of the matched functions.                                                                                                |
| **Evidence**   | `1 − e^(−matched body tokens / 200)`: one short function alike proves little, a whole exercise a lot.                                                   |
| **Identical**  | `1 − e^(−identical tokens / 200)`: the same for bodies identical as written, variable names included. It is not averaged with the functions only alike. |

`code = 1 − (1 − similarity × evidence)(1 − identical)`; pairs under 10 % are
not listed. The comparison marks each matched function `identical` or
`renamed`.

### AI

**Credited AI tools come first.** Every commit up to the graded tag is read in
full (subject, body, author, committer). A commit crediting an AI tool sets
the score to at least 95 %: tools sign their work, like
`Co-Authored-By: Claude <noreply@anthropic.com>`, `🤖 Generated with Claude
Code`, `copilot-swe-agent[bot]`, `Cursor Agent <cursoragent@cursor.com>`,
`Name (aider)`, Codex, Gemini, Devin, Windsurf, Amazon Q. Only trailers
(`Co-authored-by:`, `Generated with/by`…), 🤖 lines, the author and the
committer are checked, and Claude only through Anthropic addresses, Claude
Code or model names, so a student called Claude is not flagged. The signal
names the tool and the commits (`commits credit Claude: 3 commits (a1b2c3d …)`).

Otherwise two parts, combined as `1 − (1 − style)(1 − tells)`:

- **Style** (needs at least 5 repos): each measure is compared with the
  promo (standard deviations above the mean); the mean of the 3 highest,
  `x`, gives `1 − e^(−x/2)`. Measures: share of comment lines; share of
  comment lines that are full sentences; share of functions with a structured
  doc comment (`@brief`, `@param`, `Args:`, `Returns:`…); share of functions
  starting with a guard clause (`if (!p) return`, `raise ValueError`); mean
  identifier length; keywords and library calls almost nobody else uses
  (from 10 repos); few commits; share of the code added by the biggest
  commit.
- **Tells**, absolute: ≥ 2 typographic characters (`—`, `→`, `’`, `…`,
  emojis) in comments or strings; structured doc comments on at least half of
  the functions; commit messages that read like generated ones (`Implement …`
  with 4+ words, `feat:`, _robust_, _comprehensive_, _edge cases_…) for at
  least half of the commits; one commit holding ≥ 80 % of ≥ 150 lines (counts
  half). `tells = 1 − e^(−n/1.5)`.

The student's Suspects section lists each signal with its value and the promo
median, and the functions to read first.

## Repos

`A` clones your group after a `y` confirmation: the `--logins` list plus the
marked students, or the whole promotion when no list is given. `a` clones only the students
marked with `space` in Ranking (or in a student's page). TPs clone the graded
`submit` tag, exams the graded `submission` tag.

git runs without a terminal, so it cannot ask for your SSH key's passphrase.
Before cloning, `tpi` tries one repo: if SSH refuses the key, nothing is
cloned and the tab says so. Press `K`: `tpi` starts an `ssh-agent` if none
is running (stopped when `tpi` quits), then runs `ssh-add` on the key
`~/.ssh/config` uses for Forge and asks for the passphrase; cloning then
starts again. Running `ssh-add` before `tpi` works too.

Code is only compared between repos on disk,
so the more repos on disk, the more copies are found: a copy with a student
outside your list is only found if their repo is cloned too (mark them, or
run without `--logins`). Each repo is cloned (or fetched when
the tag changed) into `~/.cache/tpi/<slug>/repos/<login>` and checked out at
the tag of the graded submission, 6 at a time over SSH. When cloning
ends, at start-up and after a removal, every repo on disk is analysed in the
background (code similarity, AI style, `git log` up to the tag); the result
is kept until the set of repos or their commits change. The tab shows the tag, the checked-out commit and the local path; a repo
is `outdated` when a newer submit has been graded since it was cloned.
Repos already on disk from earlier runs are listed too, even when not marked,
so `r` can remove them (after a `y` confirmation).

## Cache

`~/.cache/tpi/`:

- `activities.json` : your activities;
- `<slug>/groups.json`, `sds.json`, `submissions.json`,
  `assignment-<name>.json` (submission windows) : API responses,
  reused until `R` / `--refresh`;
- `<slug>/traces/<job-id>.xml` : traces of graded and opened tags, immutable and always reused;
- `<slug>/repos/<login>` : cloned repos;
- `students/<login>-<promo>.json` : student histories downloaded with `H`.

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
- `GET /api/activities/groups/{activityUri}` returns no groups for exams. The
  git remote of each student is then built from the `repository` of their
  submissions (`/p/<tenant>/<slug>/<group>.git` →
  `<you>@git.forge.epita.fr:p/<tenant>/<slug>/<group>`), `<you>` being the
  login of your CRI session.
- `GET /api/activities/assignment/{assignmentUri}` gives the
  `openingAt` / `closingAt` of each submission definition; the assignment URI
  is the definition URI without its last segment.
- Student submission definitions are
  `<tenant>/<slug>/root/<slug>/{archi,submit}` for TPs and
  `…/submission` for exams (ACDC copies such as `acdc-submit` also exist);
  the `Assistants-*` and `dummy-submission` ones are ignored.

## Layout

```
cmd/tpi/                 CLI entry point
internal/tp/             slug parsing
internal/forge/          OAuth login and operator API client
internal/data/           dataset loading and disk cache
internal/trace/          JUnit trace parsing
internal/analysis/       risk, code similarity, AI heuristics
internal/repos/          clone / fetch / checkout at a tag, git log, SSH agent
internal/ui/             Bubble Tea interface
```
