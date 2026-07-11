#!/usr/bin/env bash
# aidev-clis uninstall: remove binaries, skills, and completions.
# Usage: uninstall.sh [cli...]     (no args = all CLIs)
set -euo pipefail

ALL_CLIS="apicli dbcli jcli logcli tcli"

BIN_INSTALL_DIR=${BIN_INSTALL_DIR:-/usr/local/bin}
CLAUDE_SKILLS_DIR=${CLAUDE_SKILLS_INSTALL_DIR:-$HOME/.claude/skills}
CODEX_SKILLS_DIR=${CODEX_SKILLS_INSTALL_DIR:-$HOME/.codex/skills}
# Completion target dirs use ${VAR-default} (no colon) so an explicitly empty
# value disables that shell instead of falling back to the system defaults.
ZSH_COMP_DIRS=${ZSH_COMP_DIRS-"/opt/homebrew/share/zsh/site-functions /usr/local/share/zsh/site-functions /usr/share/zsh/site-functions /usr/share/zsh/vendor-completions"}
BASH_COMP_DIRS=${BASH_COMP_DIRS-"/opt/homebrew/etc/bash_completion.d /usr/local/etc/bash_completion.d /etc/bash_completion.d /usr/share/bash-completion/completions"}
FISH_COMP_DIR=${FISH_COMP_DIR-"$HOME/.config/fish/completions"}

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

CLIS=$(select_clis "$@")

for cli in $CLIS; do
  echo ">> removing $cli"
  rm -f "$BIN_INSTALL_DIR/$cli"
  rm -rf "$CLAUDE_SKILLS_DIR/aidev-$cli" "$CODEX_SKILLS_DIR/aidev-$cli"
  for dir in $ZSH_COMP_DIRS;  do rm -f "$dir/_$cli"; done
  for dir in $BASH_COMP_DIRS; do rm -f "$dir/$cli"; done
  rm -f "$FISH_COMP_DIR/$cli.fish"
done

# aidev aggregator + use-aidev skill (full uninstall only).
if [ "$#" -eq 0 ]; then
  echo ">> removing aidev from $BIN_INSTALL_DIR"
  rm -f "$BIN_INSTALL_DIR/aidev"
  for sdir in "$CLAUDE_SKILLS_DIR" "$CODEX_SKILLS_DIR"; do
    rm -rf "$sdir/use-aidev"
  done
  for dir in $ZSH_COMP_DIRS;  do rm -f "$dir/_aidev"; done
  for dir in $BASH_COMP_DIRS; do rm -f "$dir/aidev"; done
  rm -f "$FISH_COMP_DIR/aidev.fish"
fi

echo "✓ uninstalled: $CLIS"
