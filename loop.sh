#!/bin/bash
set -euo pipefail

# Usage: ./loop.sh [plan] [max_iterations]
# Examples:
#   ./loop.sh              # Build mode, unlimited iterations
#   ./loop.sh 20           # Build mode, max 20 iterations
#   ./loop.sh plan         # Plan mode, unlimited iterations
#   ./loop.sh plan 5       # Plan mode, max 5 iterations

# Verify git repo
if ! git rev-parse --git-dir >/dev/null 2>&1; then
    echo "Error: not a git repository"
    exit 1
fi

# Parse arguments
if [ "${1:-}" = "plan" ]; then
    MODE="plan"
    PROMPT_FILE="PROMPT_plan.md"
    MAX_ITERATIONS=${2:-0}
elif [[ "${1:-}" =~ ^[0-9]+$ ]]; then
    MODE="build"
    PROMPT_FILE="PROMPT_build.md"
    MAX_ITERATIONS=$1
else
    MODE="build"
    PROMPT_FILE="PROMPT_build.md"
    MAX_ITERATIONS=0
fi

ITERATION=0
STALL_COUNT=0
MAX_STALLS=3
MAX_TURNS=${MAX_TURNS:-200}
MAX_RETRIES=3
LOG_DIR="logs"
CURRENT_BRANCH=$(git branch --show-current)

mkdir -p "$LOG_DIR"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Mode:        $MODE"
echo "Prompt:      $PROMPT_FILE"
echo "Branch:      $CURRENT_BRANCH"
[ "$MAX_ITERATIONS" -gt 0 ] && echo "Max:         $MAX_ITERATIONS iterations"
echo "Max turns:   $MAX_TURNS per iteration"
echo "Stall limit: $MAX_STALLS consecutive stalls"
echo "Logs:        $LOG_DIR/"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Verify prompt file exists
if [ ! -f "$PROMPT_FILE" ]; then
    echo "Error: $PROMPT_FILE not found"
    exit 1
fi

while true; do
    if [ "$MAX_ITERATIONS" -gt 0 ] && [ "$ITERATION" -ge "$MAX_ITERATIONS" ]; then
        echo "Reached max iterations: $MAX_ITERATIONS"
        break
    fi

    HEAD_BEFORE=$(git rev-parse HEAD 2>/dev/null || echo "none")
    LOG_FILE="$LOG_DIR/${MODE}_$(date +%Y%m%d_%H%M%S)_$ITERATION.json"

    # Run claude with retry + backoff for transient failures (API errors, rate limits, network)
    RETRY=0
    while true; do
        if claude -p \
            --dangerously-skip-permissions \
            --output-format=stream-json \
            --model opus \
            --max-turns "$MAX_TURNS" \
            --verbose \
            < "$PROMPT_FILE" \
            > "$LOG_FILE" 2>&1; then
            break
        else
            RETRY=$((RETRY + 1))
            if [ "$RETRY" -ge "$MAX_RETRIES" ]; then
                echo "✖ Claude failed $MAX_RETRIES times. Stopping."
                exit 1
            fi
            DELAY=$((RETRY * 30))
            echo "⚠ Claude exited with error (retry $RETRY/$MAX_RETRIES in ${DELAY}s)"
            sleep "$DELAY"
        fi
    done

    HEAD_AFTER=$(git rev-parse HEAD 2>/dev/null || echo "none")

    if [ "$HEAD_BEFORE" != "$HEAD_AFTER" ]; then
        STALL_COUNT=0
        echo "✔ New commit: $(git log -1 --pretty=%s)"

        # Build prompt creates .loop-complete when all tasks are done (disk-only, not committed)
        if [ -f ".loop-complete" ]; then
            rm -f ".loop-complete"
            echo "✔ All tasks complete. Stopping."
            exit 0
        fi
    else
        STALL_COUNT=$((STALL_COUNT + 1))
        echo "⚠ No new commit (stall $STALL_COUNT/$MAX_STALLS)"

        if [ "$STALL_COUNT" -ge "$MAX_STALLS" ]; then
            echo "✖ $MAX_STALLS consecutive stalls — agent is stuck. Stopping."
            exit 1
        fi
    fi

    ITERATION=$((ITERATION + 1))
    echo -e "\n======================== LOOP $ITERATION ========================\n"
done
