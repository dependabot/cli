## Contributing

👋 Hello!
Thanks for your interest in contributing to the `dependabot` CLI!

We accept pull requests for bug fixes and 
features that have been discussed ahead of time in an issue. 
We'd also love to hear about ideas for new features as issues.

Please do:

- [x] Check existing issues to verify that the [bug][bug issues] or 
      [feature request][feature request issues] has not already been submitted.
- [x] Open an issue if things aren't working as expected
- [x] Open an issue to propose a significant change
- [x] Open a pull request to fix a bug
- [x] Open a pull request to fix documentation about a command
- [x] Open a pull request for any issue labelled [`help wanted`][hw] or 
      [`good first issue`][gfi]

## Building the project

Prerequisites:

* Go 1.19+
* Docker

Run with:
`go run ./...`

Run tests with: 
`go test ./...`

## Submitting a pull request

1. Create a new branch: `git checkout -b my-branch-name`
2. Make your change, add tests, and ensure tests pass
3. Submit a pull request: `gh pr create --web`

Contributions to this project are [released][legal] to the public 
under the [project's open source license][license].

Please note that this project adheres to a 
[Contributor Code of Conduct][code-of-conduct]. 
By participating in this project you agree to abide by its terms.

## Releasing

Maintainers can create a new release by following these instructions:

1. Go to the [releases] page
2. Determine the appropriate version number for the release
   following [SemVer] guidelines.
3. Click the <kbd>Draft a new release</kbd> button
4. Click the <kbd>Choose a tag</kbd> button
5. Type in the version number for the release preceded by a `v`
6. Click the <kbd>Publish release</kbd> button

Publishing a release triggers the [`release.yml` workflow](./workflows/release.yml),
which builds artifacts and uploads them to the release.

## Resources

- [How to Contribute to Open Source][]
- [Using Pull Requests][]
- [GitHub Help][]

[bug issues]: https://github.com/cli/cli/issues?q=is%3Aopen+is%3Aissue+label%3Abug
[feature request issues]: https://github.com/cli/cli/issues?q=is%3Aopen+is%3Aissue+label%3Aenhancement
[hw]: https://github.com/cli/cli/labels/help%20wanted
[gfi]: https://github.com/cli/cli/labels/good%20first%20issue
[legal]: https://docs.github.com/en/free-pro-team@latest/github/site-policy/github-terms-of-service#6-contributions-under-repository-license
[license]: ../LICENSE
[code-of-conduct]: ./CODE-OF-CONDUCT.md
[releases]: https://github.com/dependabot/cli/releases
[SemVer]: https://semver.org/
[How to Contribute to Open Source]: https://opensource.guide/how-to-contribute/
[Using Pull Requests]: https://docs.github.com/en/free-pro-team@latest/github/collaborating-with-issues-and-pull-requests/about-pull-requests
[GitHub Help]: https://docs.github.com/
