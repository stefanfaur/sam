---
name: review-pr
description: Review a GitHub PR end-to-end. Use when the user asks for a PR review.
argument-hint: "[pr-number]"
---

Please review PR #$ARGUMENTS end-to-end.

Plan:
1. Fetch the PR diff with `gh pr diff $ARGUMENTS`.
2. Read the changed files in full.
3. Point out bugs, logic errors, unclear code, missing tests.
4. Suggest concrete improvements.

Focus on correctness and test coverage before style.
