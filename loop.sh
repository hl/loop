#!/bin/bash
# loop.sh — Run Claude in a loop with the same prompt. Each iteration gets a fresh context window.
#
# Usage: ./loop.sh <prompt> [--max N] [--model NAME] [--turns N]
#
#   prompt     File path or inline string (required). If a file exists at the path, its contents
#              are piped to Claude. Otherwise the string itself is used as the prompt.
#   --max      Max iterations (default: 0 = unlimited). Use to bound execution.
#   --model    Claude model name (default: sonnet).
#   --turns    Max tool-use turns per iteration (default: 200).
#
# Each iteration runs: claude -p --dangerously-skip-permissions --model MODEL --max-turns TURNS
# On failure, the loop continues to the next iteration. Ctrl+C to stop.
#
# Examples:
#   ./loop.sh prompts/build.md --max 20
#   ./loop.sh prompts/build.md --max 20 --model opus
#   ./loop.sh "Fix all TODO comments in src/" --max 5
set -euo pipefail

PROMPT="" MAX=0 MODEL=sonnet MAX_TURNS=200

while [ $# -gt 0 ]; do
    case "$1" in
        --max)       MAX="$2"; shift 2 ;;
        --model)     MODEL="$2"; shift 2 ;;
        --turns)     MAX_TURNS="$2"; shift 2 ;;
        -*)          echo "Unknown flag: $1"; exit 1 ;;
        *)           PROMPT="$1"; shift ;;
    esac
done

[ -z "$PROMPT" ] && { echo "Usage: ./loop.sh <prompt> [--max N] [--model NAME] [--turns N]"; exit 0; }

I=0
while [ "$MAX" -eq 0 ] || [ "$I" -lt "$MAX" ]; do
    if [ -f "$PROMPT" ]; then
        claude -p --dangerously-skip-permissions --model "$MODEL" --max-turns "$MAX_TURNS" < "$PROMPT" || true
    else
        echo "$PROMPT" | claude -p --dangerously-skip-permissions --model "$MODEL" --max-turns "$MAX_TURNS" || true
    fi
    I=$((I + 1))
done
