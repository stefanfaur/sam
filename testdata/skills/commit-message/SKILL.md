---
name: commit-message
description: Draft a commit message from the current staged changes.
argument-hint: "[--scope=<area>]"
---

Draft a commit message from the currently staged changes.

1. Run `git diff --cached --stat` and `git diff --cached` to see what is
   staged.
2. Summarize the change in 1 imperative-mood sentence under 70 chars.
3. Add a body paragraph (wrapped at 72 chars) explaining the why.
4. If $ARGUMENTS contains `--scope=<area>`, prefix the subject with
   `<area>: `.

Do not commit. Print the message for the user to copy.
