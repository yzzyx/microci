#!/usr/bin/env bash
# NOTE!
# We don't currently have our own credential helper, so this script requires one
# of the following to be performed before cloning/pulling will work
#
# a) The repositories are publicly available
# b) A rewrite rule for the https://-url is configured, and ssh-keys are available
# c) Credentials are stored for the URL in ~/.netrc, with the following format:
#   machine git.blah.se login my-username password my-password

function cleanup {
    exitcode=$?
    if [[ $exitcode -ne 0 ]]; then
      # If any of the git operations failed, cleanup the current directory
      rm -rf .
    fi
    exit $exitcode
}
trap cleanup EXIT

# Abort on error
set -e

# Make sure we don't get stuck in a prompt waiting for input
export GIT_TERMINAL_PROMPT=0

# Echo the commands we're executing, so that they get logged
set -x

git clone -v "$PULLREQUEST_BASE_REPO_CLONEURL" .

# These are necessary for merge commits
git config user.email 'microci@micro.ci'
git config user.name 'microci'

git checkout -b target "$PULLREQUEST_BASE_REF"
git fetch "$PULLREQUEST_HEAD_REPO_CLONEURL" +"$PULLREQUEST_HEAD_REF":source

git merge source
set +x
