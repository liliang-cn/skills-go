---
name: commit
description: Create git commits with conventional commit format. Use when the user wants to commit changes.
disable-model-invocation: true
argument-hint: "<commit-message>"
---

Create a git commit with the changes:

1. Review the git status to see what files will be committed
2. Review the git diff to understand the changes
3. Create a descriptive commit message following conventional commit format:
   - feat: for new features
   - fix: for bug fixes
   - docs: for documentation changes
   - style: for code style changes
   - refactor: for code refactoring
   - test: for adding or updating tests
   - chore: for maintenance tasks
4. Stage the relevant files (git add)
5. Create the commit (git commit)

## Arguments

- `$ARGUMENTS`: The commit message or description of changes
