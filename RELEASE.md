# Releases

There are two subpackage within the proxy-init repo with releasable artifacts. Each uses the same process for releasing but have their own tag identifiers that trigger the github release workflow. 

**Tag Identifier Format**

 * proxy-init -> `proxy-init/vX.Y.Z`
 * validator -> `validator/vX.Y.Z`

### Step 1: Prepare an appropriate log message for the release

Take a look at the changes from the last release to now within the project subdirectory you're going to release from.

`git log v2.0.0..HEAD -p proxy-init`

Prepare a commit message. For each meaningful commit since the last tag, add a single line. Use `git show` on another release tag to see the format we use.

### Step 2: Tag the release

`git tag -a proxy-init/v2.1.0`

For the commit message, use what you created in Step 1.

### Step 3: Push the tag

By default in git, tags are local so we'll need to push the tag to `origin`.

```
$ git push origin proxy-init/v2.1.0
Enumerating objects: 1, done.
Counting objects: 100% (1/1), done.
Writing objects: 100% (1/1), 431 bytes | 431.00 KiB/s, done.
Total 1 (delta 0), reused 0 (delta 0), pack-reused 0
To github.com:linkerd/linkerd2-proxy-init.git
 * [new tag]         proxy-init/v2.1.0 -> proxy-init/v2.1.0
```

There you go, a release should be running in github, you can check the Actions page.

#### Whoops! How to delete a tag

If you need to redo the release due to a workflow error or change, delete the tag you created both locally and remotely.

`git tag -d $TAGNAME`
`git push origin :refs/tags/$TAGNAME`

**Note:** If the release was successful then a docker image was also pushed for `proxy-init`. If you think this needs to be deleted, consult with your peers.

