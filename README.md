# groot - GOROOT Manager

## Method

1. Create .groot directory.
1. Download binary release for bootstrap.
1. Clone bare repo to .groot/.bare.
1. Create branch for each version to be checked out, prefixed with `groot.` to avoid any conflict.
1. Add worktree with created branch.
1. Enter branch and build.
1. Symlink `[active branch]/bin` to `.groot/bin`.
