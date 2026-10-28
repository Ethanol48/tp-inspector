# Contributing to TP inspector

Contributions are welcome from anyone at EPITA: bug fixes, new views, better
docs… This page explains how to send a change.

## 1. Fork the repo

Click **Fork** at the top right of the project page on GitHub. You get your
own copy, where you can push freely. No access request is needed.

If you have a question first, send me a message on Discord: **`ethanol48`** /
my login is: **ethan.rouimi** (you can find me in the server 2029 / assistants
server).

## 2. Make your change

```sh
git clone https://github.com/<your-username>/tp-inspector.git
cd tp-inspector
git remote add upstream https://github.com/Ethanol48/tp-inspector.git
git switch -c my-feature
```

To start from the latest version, update `master` before creating the branch:
`git fetch upstream && git rebase upstream/master`.

- Code, comments and UI strings are in English.
- Keep the style of the surrounding code.
- Before pushing, check that it builds and the tests pass:

  ```sh
  gofmt -l .        # should print nothing (gofmt -w . fixes it)
  go vet ./...
  go test ./...
  ```

## 3. Open a pull request

```sh
git push -u origin my-feature
```

Git prints a link to create the pull request; or open your fork on GitHub,
**Contribute → Open pull request**, base `master` of this repo, compare
`my-feature`. `master` is protected: every change goes through a pull request.

In the description, say:

- what it changes and why;
- for the TUI, how you tested it (which assignment, which tab or view).

I review it, may ask for changes (push new commits on the same branch, the
pull request updates itself), then merge it.

For a bigger change, or an idea you are not sure about, open an issue or ask
me on Discord first, so you don't work on something that won't be merged.

## Never commit student data or credentials

No login lists, cached traces, cloned repos, exported CSVs or tokens. The
cache lives in `~/.cache/tpi`, outside the repo, and `.gitignore` covers the
usual files, but always check `git status` before committing.

## License

By contributing, you agree that your contributions are licensed under the
[MIT license](LICENSE) of the project.
