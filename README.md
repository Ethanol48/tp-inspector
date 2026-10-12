# TP inspector

A terminal UI to explore an EPITA assignment on Forge **operator**: promo
statistics, rankings, each student's trace as a collapsible test tree (like
the intranet), their submission history and grades across the year, and
hints of plagiarism based on test traces.

You need an EPITA account with access to the assignment on operator
(assistants / staff).

## 1. Compile

Requires **Go 1.26+**
and `git`.

```sh
git clone https://gitlab.cri.epita.fr/ethan.rouimi/tp-inspector.git
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
3. The page says *"OK - you can close this tab now."* and the terminal prints
   `OK: logged in.`

The session is saved in `~/.cri-tokens` (shared with `poulpy`) and refreshed
automatically. When it fully expires, `tpi` opens the login page again
before starting.

## 3. Use it

Pass the slug of an assignment:

```sh
./tpi prog-205-p-01-2030
```

`prog-205-p-01-2030` = subject `prog`, year `2`, bimester `05`, kind `p` (TP;
`e` exam, `r` retake), number `01`, class of `2030`.

To focus on some students only (e.g. your group), pass a text file of logins
with `--logins` (before the slug):

```sh
./tpi --logins my-group.txt prog-205-p-01-2030
```

```
# my-group.txt: one login per line, # for comments
firstname.lastname
other.student@epita.fr
```

The Ranking then shows **only those students** (plus any you mark with
`space`); press `f` to switch between them and the whole class. The rest of
the tool (statistics, Tests, Traces) still uses the whole class, so
similarities with students outside your list are still detected.

The first load downloads the submissions and traces of the class (cached in
`~/.cache/tpi`, so later runs are instant). Then:

| Key | Action |
|-----|--------|
| `←/→`, `tab`, `1`–`5` | Switch tab: Overview, Ranking, Tests, Traces, Repos |
| `↑/↓`, `enter`, `esc` | Move, open, go back |
| `enter` on a student | Their trace as a tree, their tags, suspicious pairs |
| `H` on a student | Their grades in every TP / exam, by academic year |
| `s` / `/` | Sort / filter the Ranking |
| `space`, then `c` | Mark two students and compare them |
| `?` | Every key |
| `q` | Quit |

To free disk space later: `tpi clean`.

Scores are **hints**, not proof: always read the code before concluding.

## More

[DOCUMENTATION.md](DOCUMENTATION.md) covers every flag (`--logins`,
`--refresh`…), tab, key, the similarity scoring, repo cloning, the cache,
`tpi clean` and notes on the operator API.

## Contribute

[CONTRIBUTING.md](CONTRIBUTING.md) explains every step.

## License

[MIT](LICENSE): free to use, modify and share, as long as the copyright notice
is kept.
