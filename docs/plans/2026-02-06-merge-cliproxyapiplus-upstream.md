# 合并 CLIProxyAPIPlus 上游 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将子模块 third_party/CLIProxyAPIPlus 合并 https://github.com/router-for-me/CLIProxyAPIPlus 的 main 分支更新，并在主仓库提交子模块指针更新（不修改 .gitmodules）。

**Architecture:** 在子模块仓库内添加 upstream 远端并 fetch main，然后执行 merge（允许 merge commit）。合并完成后回到主仓库记录 gitlink 变化并提交。

**Tech Stack:** Git, git submodule

---

### Task 1: 确认基线与远端配置

**Files:**
- Read: .gitmodules
- Read: third_party/CLIProxyAPIPlus/.git/config

**Step 1: 查看子模块与主仓库状态**

Run: `git status -sb`
Expected: 工作区干净或仅显示子模块变更

**Step 2: 查看子模块远端**

Run: `git -C third_party/CLIProxyAPIPlus remote -v`
Expected: 至少看到 origin 的 fetch/push URL

**Step 3: 记录当前子模块提交**

Run: `git submodule status`
Expected: third_party/CLIProxyAPIPlus 指向当前提交

### Task 2: 添加 upstream 远端并拉取

**Files:**
- Modify: third_party/CLIProxyAPIPlus/.git/config

**Step 1: 若 upstream 不存在则添加**

Run: `git -C third_party/CLIProxyAPIPlus remote add upstream https://github.com/router-for-me/CLIProxyAPIPlus.git`
Expected: 无输出；如已存在，改用 Step 2

**Step 2: 若 upstream 已存在则更新 URL**

Run: `git -C third_party/CLIProxyAPIPlus remote set-url upstream https://github.com/router-for-me/CLIProxyAPIPlus.git`
Expected: 无输出

**Step 3: 拉取 upstream/main**

Run: `git -C third_party/CLIProxyAPIPlus fetch upstream`
Expected: 输出新的分支/提交信息

### Task 3: 合并 upstream/main 到子模块 main

**Files:**
- Modify: third_party/CLIProxyAPIPlus/.git/HEAD
- Modify: third_party/CLIProxyAPIPlus/.git/ORIG_HEAD
- Modify: third_party/CLIProxyAPIPlus/.git/refs/heads/main

**Step 1: 切到 main 分支**

Run: `git -C third_party/CLIProxyAPIPlus checkout main`
Expected: `Already on 'main'`

**Step 2: 合并 upstream/main（允许 merge commit）**

Run: `git -C third_party/CLIProxyAPIPlus merge --no-ff -m "Merge upstream/main" upstream/main`
Expected: 若无更新则显示 `Already up to date.`；否则生成合并提交

**Step 3: 若出现冲突，先解决再继续**

Run: `git -C third_party/CLIProxyAPIPlus status -sb`
Expected: 若有冲突，按提示解决并 `git -C third_party/CLIProxyAPIPlus add <files>` 后 `git -C third_party/CLIProxyAPIPlus commit`

### Task 4: 在主仓库更新子模块指针并提交

**Files:**
- Modify: third_party/CLIProxyAPIPlus (gitlink)

**Step 1: 确认主仓库检测到子模块变更**

Run: `git status -sb`
Expected: 显示 `modified: third_party/CLIProxyAPIPlus` 或类似提示

**Step 2: 暂存子模块指针**

Run: `git add third_party/CLIProxyAPIPlus`
Expected: 无输出

**Step 3: 提交主仓库变更**

Run: `git commit -m "chore: sync CLIProxyAPIPlus submodule with upstream"`
Expected: 生成新的提交记录
