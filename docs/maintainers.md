## Maintainer docs

This documentation is targeting maintainers of the Dependabot CLI.

### Creating a release

- Go to https://github.com/dependabot/cli/releases
- Click `Draft a new release`
- Click `Choose a tag`
- In the input, type `v1.<minor>.<patch>` choosing the next minor or patch
   - It really doesn't matter much which one you bump, but if it's a fix to the previous release patch makes sense.
   - Don't bump the major version unless there were large breaking changes.
-  Click `Generate release notes`
   - If you want, you can delete lines that are minor like changes to workflows, README typo fixes, etc.  
-  Click `Publish release`
-  Monitor the [Release binary builder](https://github.com/dependabot/cli/actions/workflows/release.yml)https://github.com/dependabot/cli/actions/workflows/release.yml, it sometimes fails and needs to re-run

 If anything goes wrong, like you've typed in something non-sensical as the version number by mistake, just delete the release/tag and create a new one.
 
