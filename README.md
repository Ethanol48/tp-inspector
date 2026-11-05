# TP inspector

A terminal UI to explore an EPITA assignment on Forge **operator**: promo
statistics, rankings, each student's trace as a collapsible test tree (like
the intranet), their submission history and grades across the year, and
hints of cheating: students reaching a high grade suspiciously early, at the
last minute or in one jump, repos sharing code, and code that looks
AI-generated.

You need an EPITA account with access to the assignment on operator
(assistants / staff).

## 1. Compile

Requires **Go 1.26+**
and `git`.

```sh
git clone https://github.com/Ethanol48/tp-inspector.git
cd tp-inspector
go build -o tpi ./cmd/tpi
```

This creates the `tpi` binary in the current directory. To run it from
anywhere, use `go install ./cmd/tpi` instead (it goes to `~/go/bin`, which must
be in your `PATH`).

## 2. Log in

```sh
./tpi login
```

1. `tpi` prints `>>> Log in at: http://…` and opens that page in your browser
   (open it by hand if nothing happens).
2. Log in with your EPITA (CRI) account.
3. The page says _"OK - you can close this tab now."_ and the terminal prints
   `OK: logged in.`

It does this twice: once for operator, once for the CRI API (the classes
behind `F`), which needs another OAuth client. If you are already logged in
to the CRI in your browser, the second page only redirects.

The sessions are saved in `~/.cri-tokens` (shared with `poulpy`) and
`~/.cri-api-tokens`, and refreshed automatically. When one fully expires,
`tpi` opens the login page again when it needs it.

## 3. Use it

Pass the slug of an assignment:

```sh
./tpi prog-205-p-01-2030
```

`prog-205-p-01-2030` = subject `prog`, year `2`, bimester `05`, kind `p` (TP;
`e` exam, `r` retake), number `01`, promo `2030`.

To focus on some students only (your class/group), pass a text file of logins
with `--logins` (before the slug):

```sh
./tpi --logins my-group.txt prog-205-p-01-2030
```

```
# my-group.txt: one login per line, # for comments
firstname.lastname
other.student@epita.fr
```

Those students (plus any you mark with `space`) form **your group**:

- the **Overview** (mean, median, histogram…) starts on them, with the promo
  mean and median next to theirs; `f` switches to the whole promo;
- Tests adds the group pass rate and the gap with the promo (`s` sorts by gap);
- Ranking and Code show only the group; `f` switches to the whole promo.

Instead of a file, you can pick your classes from the CRI: `--select-group`
opens the picker as soon as the assignment is loaded, and `F` opens it at any
time. It shows the campuses, then only the classes of the activity's semester
(`prog-206-p-01-2030` → S3 classes: SPE, not SUP nor ING1); `space` marks
several classes, `enter` makes their members your group.

```sh
./tpi --select-group prog-206-p-01-2030
```

Suspicion is shown as a level (**high** / **medium** / **low**) with a reason
in words. It combines three signals, `1 − (1−risk)(1−code)(1−AI)`, so a
student with several signals ranks above one with a single strong signal:

- **Risk**, for everyone, from the submissions alone: a high grade reached
  in the first 10 % or the last 3 % of the submission window (read from the
  API), or in one jump (first submission already ≥ 90 %); timing and jump add up. Good students who
  finish early come up too: that is where copies hide, not among students
  who fail.
- **Code**: function bodies alike between two repos, renaming and layout
  ignored, the subject's skeleton left out (C and Python). Names and
  parameters come from the subject and are the same for everyone, so only
  bodies are compared.
- **AI**: a coding style far from the rest of the promo (comment sentences, `@param`
  doc blocks, guard clauses, long names, constructs nobody else uses), the
  whole history in one commit, typographic characters, generated-looking
  commit messages, and above all commits that credit an AI tool
  (`Co-Authored-By: Claude`, `Generated with Claude Code`, Copilot / Cursor
  agents…), which put the score at 95 % or more.

Code and AI need the repos on disk: press `A` in the **Repos** tab to clone
your group, which is the `--logins` list plus marked students, or the whole
promotion without a list (a copy is only found between two repos on disk),
then the analysis runs by itself. Sort the Ranking by risk (`s`) to know whom
to check first, then by suspicion once their repos are analysed.

The first load downloads the submissions and traces of the promo (cached in
`~/.cache/tpi`, so later runs are instant). Then:

| Key                   | Action                                                                                                             |
| --------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `←/→`, `tab`, `1`–`5` | Switch tab: Overview, Ranking, Tests, Code, Repos                                                                  |
| `↑/↓`, `enter`, `esc` | Move, open, go back                                                                                                |
| `enter` on a student  | Their trace as a tree, their tags, their risk / AI / code signals                                                  |
| `A` in Repos          | Clone your `--logins` group (else the promotion) and analyse the code (`K` unlocks a passphrase-protected SSH key) |
| `H` on a student      | Their grades in every TP / exam, by academic year                                                                  |
| `F`                   | Pick your group among the CRI classes of the activity's semester (similar to `--logins`)                           |
| `s` / `/`             | Sort / filter the Ranking                                                                                          |
| `space`, then `c`     | Mark two students and compare them                                                                                 |
| `?`                   | Every key                                                                                                          |
| `q`                   | Quit                                                                                                               |

To free disk space later: `tpi clean`.

Scores are **hints**, not proof: always read the code before concluding.

## More

[DOCUMENTATION.md](DOCUMENTATION.md) covers every flag (`--logins`,
`--select-group`, `--refresh`…), tab, key, the CRI classes, the suspicion
scoring, repo cloning, the cache, `tpi clean` and notes on the operator API.

## Contribute

[CONTRIBUTING.md](CONTRIBUTING.md) explains every step.

## License

[MIT](LICENSE): free to use, modify and share, as long as the copyright notice
is kept.

## Thank you!!

Thanks to all the assistants who made tpi better:

<p>
  <a href="https://github.com/EvanBaron"><img src="https://github.com/EvanBaron.png?size=100" width="64" height="64" alt="EvanBaron" title="EvanBaron"></a>
  <a href="https://github.com/FlkDPis"><img src="https://github.com/FlkDPis.png?size=100" width="64" height="64" alt="FlkDPis" title="FlkDPis"></a>
  <a href="https://github.com/Carl-Excoffier"><img src="https://github.com/Carl-Excoffier.png?size=100" width="64" height="64" alt="Carl-Excoffier" title="Carl-Excoffier"></a>
  <a href="https://github.com/Nathzxu"><img src="https://github.com/Nathzxu.png?size=100" width="64" height="64" alt="Nathzxu" title="Nathzxu"></a>
</p>
