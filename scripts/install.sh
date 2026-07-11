#!/usr/bin/env bash
# aidev-clis install: build CLIs, copy skills into agent dirs, install completions.
# Usage: install.sh [cli...]     (no args = all CLIs)
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ALL_CLIS="apicli dbcli jcli logcli tcli"
# Release archives ship prebuilt binaries in bin/ and no go.mod; a source
# checkout has go.mod. That marker picks the mode: prebuilt installs skip
# `go build` and everything else (skills, completions, PATH) is identical.
PREBUILT=0
[ -f "$REPO/go.mod" ] || PREBUILT=1
# Skills shipped by the old monolithic `aidev` repo that this repo supersedes
# and no longer ships. They are removed from the skill dirs on every install so
# a fresh install fully migrates an old setup (set AIDEV_STALE_SKILLS= to skip).
# (aidev-tcli was one such old skill; this repo now ships its own — see skills/.)
STALE_SKILLS=${AIDEV_STALE_SKILLS-"aidev"}

BIN_INSTALL_DIR=${BIN_INSTALL_DIR:-/usr/local/bin}
CLAUDE_SKILLS_DIR=${CLAUDE_SKILLS_INSTALL_DIR:-$HOME/.claude/skills}
CODEX_SKILLS_DIR=${CODEX_SKILLS_INSTALL_DIR:-$HOME/.codex/skills}
# Completion target dirs use ${VAR-default} (no colon) so an explicitly empty
# value disables that shell instead of falling back to the system defaults.
ZSH_COMP_DIRS=${ZSH_COMP_DIRS-"/opt/homebrew/share/zsh/site-functions /usr/local/share/zsh/site-functions /usr/share/zsh/site-functions /usr/share/zsh/vendor-completions"}
BASH_COMP_DIRS=${BASH_COMP_DIRS-"/opt/homebrew/etc/bash_completion.d /usr/local/etc/bash_completion.d /etc/bash_completion.d /usr/share/bash-completion/completions"}
FISH_COMP_DIR=${FISH_COMP_DIR-"$HOME/.config/fish/completions"}

# Validate requested CLIs against ALL_CLIS; no args selects all.
select_clis() {
  if [ "$#" -eq 0 ]; then echo "$ALL_CLIS"; return; fi
  local c
  for c in "$@"; do
    case " $ALL_CLIS " in
      *" $c "*) ;;
      *) echo "error: unknown cli '$c' (valid: $ALL_CLIS)" >&2; exit 2 ;;
    esac
  done
  echo "$@"
}

# Completions are the optional tail of an install. A write that fails (e.g. an
# existing but unwritable system dir) is downgraded to a warning so it never
# aborts a run whose binary + skills already landed.
write_completion() {
  local cli="$1" shell="$2" target="$3"
  if "$BIN_INSTALL_DIR/$cli" completion "$shell" > "$target" 2>/dev/null; then
    wrote=1
  else
    echo "   warn: cannot write $target (skipped)"
  fi
}

install_completion() {
  local cli="$1" dir wrote=0
  [ "${AIDEV_INSTALL_COMPLETION:-1}" = "0" ] && return 0
  for dir in $ZSH_COMP_DIRS; do
    [ -d "$dir" ] || continue
    write_completion "$cli" zsh "$dir/_$cli"
  done
  for dir in $BASH_COMP_DIRS; do
    [ -d "$dir" ] || continue
    write_completion "$cli" bash "$dir/$cli"
  done
  if [ -d "$FISH_COMP_DIR" ]; then
    write_completion "$cli" fish "$FISH_COMP_DIR/$cli.fish"
  fi
  [ "$wrote" = 1 ] || echo "   note: no completion dir found for $cli (override ZSH_COMP_DIRS/BASH_COMP_DIRS/FISH_COMP_DIR)"
}

# Example config(s) copied into each installed skill dir, so an agent using the
# skill (which has no repo checkout) can read a full, commented sample. examples/
# stays the single source; the release archive ships it (see .goreleaser.yaml).
skill_examples() {
  case "$1" in
    aidev-apicli) echo "apicli.yaml actors.yaml" ;;
    aidev-dbcli)  echo "dbcli.yaml" ;;
    aidev-jcli)   echo "jcli.yaml" ;;
    aidev-logcli) echo "logcli.yaml" ;;
    aidev-tcli)   echo "tcli-case.yaml tcli-case-minimal.yaml" ;;
    use-aidev)    echo ".aidev.yaml" ;;
  esac
}
# Copy a skill's bundled examples into its just-installed dir (missing files are
# skipped, so this is safe under `set -e` even if examples/ is trimmed).
copy_skill_examples() {
  local skill="$1" dest="$2" ex
  for ex in $(skill_examples "$skill"); do
    [ -f "$REPO/examples/$ex" ] && cp "$REPO/examples/$ex" "$dest/"
  done
}

CLIS=$(select_clis "$@")
mkdir -p "$BIN_INSTALL_DIR" "$REPO/bin"

# Drop superseded old-repo skills so a fresh install migrates cleanly.
for sdir in "$CLAUDE_SKILLS_DIR" "$CODEX_SKILLS_DIR"; do
  for stale in $STALE_SKILLS; do
    [ -e "$sdir/$stale" ] || continue
    echo ">> removing stale skill $stale from $sdir"
    rm -rf "${sdir:?}/${stale:?}"
  done
done

for cli in $CLIS; do
  if [ "$PREBUILT" = 0 ]; then
    echo ">> building $cli"
    ( cd "$REPO" && go build -o "bin/$cli" "./cmd/$cli" )
  fi

  echo ">> installing $cli to $BIN_INSTALL_DIR"
  install -m 0755 "$REPO/bin/$cli" "$BIN_INSTALL_DIR/"

  for sdir in "$CLAUDE_SKILLS_DIR" "$CODEX_SKILLS_DIR"; do
    echo ">> installing skill aidev-$cli to $sdir"
    mkdir -p "$sdir"
    rm -rf "$sdir/aidev-$cli"
    cp -R "$REPO/skills/aidev-$cli" "$sdir/"
    copy_skill_examples "aidev-$cli" "$sdir/aidev-$cli"
  done

  install_completion "$cli"
done

# aidev: the cross-CLI discovery aggregator (not in ALL_CLIS). Installed only on
# a full install (no explicit cli args), alongside its `use-aidev` skill.
if [ "$#" -eq 0 ]; then
  if [ "$PREBUILT" = 0 ]; then
    echo ">> building aidev"
    ( cd "$REPO" && go build -o "bin/aidev" "./cmd/aidev" )
  fi
  echo ">> installing aidev to $BIN_INSTALL_DIR"
  install -m 0755 "$REPO/bin/aidev" "$BIN_INSTALL_DIR/"
  for sdir in "$CLAUDE_SKILLS_DIR" "$CODEX_SKILLS_DIR"; do
    echo ">> installing skill use-aidev to $sdir"
    mkdir -p "$sdir"
    rm -rf "$sdir/use-aidev"
    cp -R "$REPO/skills/use-aidev" "$sdir/"
    copy_skill_examples "use-aidev" "$sdir/use-aidev"
  done
  install_completion "aidev"
fi

# A sudo run must not leave root-owned files under the invoking user's $HOME —
# they break the next non-sudo install's rm -rf. Hand the user-scoped dirs
# (skills + fish completions; NOT the system completion dirs) back to that user.
if [ "$(id -u)" = 0 ] && [ -n "${SUDO_USER:-}" ]; then
  for d in "$CLAUDE_SKILLS_DIR" "$CODEX_SKILLS_DIR" "$FISH_COMP_DIR"; do
    if [ -d "$d" ]; then
      chown -R "$SUDO_USER" "$d" || echo "   warn: cannot chown $d back to $SUDO_USER"
    fi
  done
fi

echo
echo "✓ installed: $CLIS"
echo "  binaries: $BIN_INSTALL_DIR/{$(echo "$CLIS" | tr ' ' ',')}"
echo "  skills:   $CLAUDE_SKILLS_DIR and $CODEX_SKILLS_DIR (aidev-<cli>)"
if [ "$#" -eq 0 ]; then
  echo "  + aidev (cross-CLI discovery) binary and the use-aidev skill"
fi
case ":$PATH:" in
  *":$BIN_INSTALL_DIR:"*) ;;
  *) echo "  PATH note: add $BIN_INSTALL_DIR to PATH to run the CLIs" ;;
esac
echo "  restart your shell (or run compinit in zsh) to pick up completions"
