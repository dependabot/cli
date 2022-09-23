# Releasing the CLI

- Go to the [releases] page and note the last release. Decide if this will be major, minor, or patch.
  - During our pre-release we are bumping patch
  - Choose minor for new features and fixes
  - Choose patch if you are back-porting a fix to an older version
  - Choose major for breaking changes to command line flags or any other ways the CLI works
- Click [Draft a new release]
- Click "Choose a tag"
- Type in the new version preceded with a `v`
- Click "Create new tag" which is just under where you typed
- Optionally create a Title and add release notes
- Click "Publish release"

The `release.yml` workflow will kick off and add binaries to the release after a couple of minutes.

Feel free to automate this in the future!

[releases]: https://github.com/dependabot/cli/releases
[Draft a new release]: https://github.com/dependabot/cli/releases/new
