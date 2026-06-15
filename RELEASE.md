# Releases

There are two subpackage within the proxy-init repo with releasable artifacts.
Each uses the same process for releasing but have their own tag identifiers
that trigger the github release workflow.

The Tag identifier format is:

    * proxy-init -> `proxy-init/vX.Y.Z`
    * validator -> `validator/vX.Y.Z`
    * cni-plugin -> `cni-plugin/vX.Y.Z`

## Step 1: Prepare an appropriate log message for the release

Take a look at the changes from the last release to now within the project
subdirectory you're going to release from as well as `internal` if the project
relies on code living there.

`git log proxy-init/v2.1.0..HEAD -p proxy-init`
`git log proxy-init/v2.1.0..HEAD -p internal`

Prepare a commit message. For each meaningful commit since the last tag, add
a single line. See `git show proxy-init/v2.2.0` as an example the format we use.

Note: currently `validator` doesn't rely on `internal`, while `cni-plugin` and
`proxy-init` do.

## Step 2: Update version in Cargo.toml (only for validator)

Update the version number in `validator/Cargo.toml` (without the `v` prefix).

## Step 3: Tag the release

First, find the current latest version tag for the subproject

`git tag -l 'proxy-init/*' --sort=-"v:refname" | head -n 1`

Increase the version with a major, minor, or patch according to the changes made

`git tag -a proxy-init/v2.x.x`

For the commit message, use what you created in Step 1.

## Step 4: Push the tag

By default in git, tags are local so we'll need to push the tag to `origin`.

`git push origin proxy-init/v2.2.0`

There you go, a release should be running in github, you can check the Actions
page.

### Whoops! How to delete a tag

If you need to redo the release due to a workflow error or change, delete
the tag you created both locally and remotely.

`git tag -d $TAGNAME`
`git push origin :refs/tags/$TAGNAME`

**Note:** If the release was successful then a docker image was also pushed
for `proxy-init` and `cni-plugin`. If you think this needs to be deleted, consult
with your peers.
