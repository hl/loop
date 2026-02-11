#!/bin/bash
set -euo pipefail

# Usage: ./loop.sh [--profile NAME] [plan] [max_iterations]
# Examples:
#   ./loop.sh              # Build mode, unlimited iterations
#   ./loop.sh 20           # Build mode, max 20 iterations
#   ./loop.sh plan         # Plan mode, unlimited iterations
#   ./loop.sh plan 5       # Plan mode, max 5 iterations
#   ./loop.sh --profile claude-strong plan 3

# Verify git repo
if ! git rev-parse --git-dir >/dev/null 2>&1; then
    echo "Error: not a git repository"
    exit 1
fi

# Parse arguments
PROFILE=""
POSITIONAL=()
while [ $# -gt 0 ]; do
    case "$1" in
        --profile)
            PROFILE="${2:-}"
            if [ -z "$PROFILE" ]; then
                echo "Error: --profile requires a name"
                exit 1
            fi
            shift 2
            ;;
        --profile=*)
            PROFILE="${1#*=}"
            if [ -z "$PROFILE" ]; then
                echo "Error: --profile requires a name"
                exit 1
            fi
            shift
            ;;
        --help|-h)
            echo "Usage: ./loop.sh [--profile NAME] [plan] [max_iterations]"
            exit 0
            ;;
        *)
            POSITIONAL+=("$1")
            shift
            ;;
    esac
done

is_number() {
    [[ "$1" =~ ^[0-9]+$ ]]
}

DEFAULT_MODEL="opus"
DEFAULT_MAX_TURNS=200
DEFAULT_MAX_RETRIES=3
DEFAULT_MAX_STALLS=3
DEFAULT_LOG_DIR="loop/logs"
DEFAULT_PROMPT_PLAN="loop/PROMPT_plan.claude.md"
DEFAULT_PROMPT_BUILD="loop/PROMPT_build.claude.md"

LOOP_CONFIG_FILE="loop/config.ini"
PROFILE_MODEL=""
PROFILE_PROMPT_PLAN=""
PROFILE_PROMPT_BUILD=""
PROFILE_LOG_DIR=""
PROFILE_FOUND=0

if [ ! -f "$LOOP_CONFIG_FILE" ]; then
    echo "Error: $LOOP_CONFIG_FILE not found"
    exit 1
fi

CURRENT_SECTION=""
while IFS= read -r line || [ -n "$line" ]; do
    line="${line%%#*}"
    line="${line%%;*}"
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [ -z "$line" ] && continue

    if [[ "$line" =~ ^\[(.+)\]$ ]]; then
        CURRENT_SECTION="${BASH_REMATCH[1]}"
        if [ -n "$PROFILE" ] && [ "$CURRENT_SECTION" = "$PROFILE" ]; then
            PROFILE_FOUND=1
        fi
        continue
    fi

    if [[ "$line" =~ ^([a-zA-Z_]+)[[:space:]]*=(.*)$ ]]; then
        key="${BASH_REMATCH[1]}"
        value="${BASH_REMATCH[2]}"
        value="${value#"${value%%[![:space:]]*}"}"
        value="${value%"${value##*[![:space:]]}"}"

        if [ "$CURRENT_SECTION" = "defaults" ]; then
            case "$key" in
                model) DEFAULT_MODEL="$value" ;;
                max_turns)
                    if ! is_number "$value"; then
                        echo "Error: defaults.max_turns must be an integer"
                        exit 1
                    fi
                    DEFAULT_MAX_TURNS="$value"
                    ;;
                max_retries)
                    if ! is_number "$value"; then
                        echo "Error: defaults.max_retries must be an integer"
                        exit 1
                    fi
                    DEFAULT_MAX_RETRIES="$value"
                    ;;
                max_stalls)
                    if ! is_number "$value"; then
                        echo "Error: defaults.max_stalls must be an integer"
                        exit 1
                    fi
                    DEFAULT_MAX_STALLS="$value"
                    ;;
                log_dir) DEFAULT_LOG_DIR="$value" ;;
                prompt_plan) DEFAULT_PROMPT_PLAN="$value" ;;
                prompt_build) DEFAULT_PROMPT_BUILD="$value" ;;
            esac
        elif [ -n "$PROFILE" ] && [ "$CURRENT_SECTION" = "$PROFILE" ]; then
            case "$key" in
                model) PROFILE_MODEL="$value" ;;
                prompt_plan) PROFILE_PROMPT_PLAN="$value" ;;
                prompt_build) PROFILE_PROMPT_BUILD="$value" ;;
                log_dir) PROFILE_LOG_DIR="$value" ;;
            esac
        fi
    fi
done < "$LOOP_CONFIG_FILE"

if [ -n "$PROFILE" ] && [ "$PROFILE_FOUND" -eq 0 ]; then
    echo "Error: profile '$PROFILE' not found in $LOOP_CONFIG_FILE"
    exit 1
fi

RESOLVED_PROMPT_PLAN=${PROFILE_PROMPT_PLAN:-$DEFAULT_PROMPT_PLAN}
RESOLVED_PROMPT_BUILD=${PROFILE_PROMPT_BUILD:-$DEFAULT_PROMPT_BUILD}

if [ "${POSITIONAL[0]:-}" = "plan" ]; then
    MODE="plan"
    PROMPT_FILE="$RESOLVED_PROMPT_PLAN"
    MAX_ITERATIONS=${POSITIONAL[1]:-0}
elif [[ "${POSITIONAL[0]:-}" =~ ^[0-9]+$ ]]; then
    MODE="build"
    PROMPT_FILE="$RESOLVED_PROMPT_BUILD"
    MAX_ITERATIONS=${POSITIONAL[0]}
else
    MODE="build"
    PROMPT_FILE="$RESOLVED_PROMPT_BUILD"
    MAX_ITERATIONS=0
fi

ITERATION=0
STALL_COUNT=0
MAX_STALLS=$DEFAULT_MAX_STALLS
MAX_TURNS=$DEFAULT_MAX_TURNS
MAX_RETRIES=$DEFAULT_MAX_RETRIES
LOG_DIR=${PROFILE_LOG_DIR:-$DEFAULT_LOG_DIR}
CURRENT_BRANCH=$(git branch --show-current)

LOOP_MODEL=${PROFILE_MODEL:-$DEFAULT_MODEL}

if ! command -v claude >/dev/null 2>&1; then
    echo "Error: claude not found in PATH"
    exit 1
fi

CLI_CMD=(claude -p --dangerously-skip-permissions --output-format=stream-json --model "$LOOP_MODEL" --max-turns "$MAX_TURNS" --verbose)

mkdir -p "$LOG_DIR"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Mode:        $MODE"
echo "Prompt:      $PROMPT_FILE"
echo "Branch:      $CURRENT_BRANCH"
echo "Config:      $LOOP_CONFIG_FILE"
if [ -n "$PROFILE" ]; then
    echo "Profile:     $PROFILE"
else
    echo "Profile:     none"
fi
echo "Model:       $LOOP_MODEL"
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

    # Run agent with retry + backoff for transient failures (API errors, rate limits, network)
    RETRY=0
    while true; do
        if "${CLI_CMD[@]}" \
            < "$PROMPT_FILE" \
            > "$LOG_FILE" 2>&1; then
            break
        else
            RETRY=$((RETRY + 1))
            if [ "$RETRY" -ge "$MAX_RETRIES" ]; then
                echo "✖ Agent failed $MAX_RETRIES times. Stopping."
                exit 1
            fi
            DELAY=$((RETRY * 30))
            echo "⚠ Agent exited with error (retry $RETRY/$MAX_RETRIES in ${DELAY}s)"
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
