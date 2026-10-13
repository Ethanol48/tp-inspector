# Contributing to TP inspector

Contributions are welcome from anyone at EPITA: bug fixes, new views, better
docs… This page explains how to get access and how to send a change.

## 1. Get access

Forking is not available on this GitLab, so contributors work on branches of
this repo directly. For that you need to be a member of the project:

1. Send me a message on Discord: **`ethanol48`** / my login is: **ethan.rouimi** (you can find me in the server 2029 / assistants server), with your EPITA
   login (`firstname.lastname`).
2. I add you to the project as **Developer**. You get an email from GitLab
   when it is done.

As a Developer you can create branches, push to them and open merge requests.
`master` is protected: only the maintainer can merge into it, so every change
goes through a merge request.

## 2. Make your change

```sh
git clone git@gitlab.cri.epita.fr:ethan.rouimi/tp-inspector.git
cd tp-inspector
git switch -c my-feature
```

- Code, comments and UI strings are in English.
- Keep the style of the surrounding code.
- Before pushing, check that it builds and the tests pass:

  ```sh
  gofmt -l .        # should print nothing (gofmt -w . fixes it)
  go vet ./...
  go test ./...
  ```

## 3. Open a merge request

```sh
git push -u origin my-feature
```

Git prints a link to create the merge request; or open the project on GitLab,
**Merge requests → New merge request**, source `my-feature`, target `master`.

In the description, say:

- what it changes and why;
- for the TUI, how you tested it (which assignment, which tab or view).

I review it, may ask for changes (push new commits on the same branch, the
merge request updates itself), then merge it.

For a bigger change, or an idea you are not sure about, open an issue or ask
me on Discord first, so you don't work on something that won't be merged.

## Never commit student data or credentials

No login lists, cached traces, cloned repos, exported CSVs or tokens. The
cache lives in `~/.cache/tpi`, outside the repo, and `.gitignore` covers the
usual files, but always check `git status` before committing.

## License

By contributing, you agree that your contributions are licensed under the
[MIT license](LICENSE) of the project.
