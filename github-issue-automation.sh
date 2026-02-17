#!/bin/bash
set -euo pipefail

# GitHub Issue Automation Script
# Usage: ./github-issue-automation.sh <owner/repo> [--poll-interval SECONDS]
#
# This script monitors GitHub issues and automatically processes new or reopened issues
# by running loop.sh plan and build, then closing the issue.

# Default configuration
POLL_INTERVAL=60  # seconds
STATE_FILE=".github-issue-state"
LOG_DIR="loop/logs/github-automation"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Parse arguments
if [ $# -lt 1 ]; then
    echo "Usage: $0 <owner/repo> [--poll-interval SECONDS]"
    echo ""
    echo "Example:"
    echo "  $0 myorg/myrepo"
    echo "  $0 myorg/myrepo --poll-interval 30"
    exit 1
fi

REPO="$1"
shift

while [ $# -gt 0 ]; do
    case "$1" in
        --poll-interval)
            POLL_INTERVAL="${2:-60}"
            if ! [[ "$POLL_INTERVAL" =~ ^[0-9]+$ ]]; then
                echo "Error: poll interval must be a number"
                exit 1
            fi
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Verify dependencies
if ! command -v gh >/dev/null 2>&1; then
    echo "Error: GitHub CLI (gh) not found. Install it from https://cli.github.com/"
    exit 1
fi

if [ ! -f "$SCRIPT_DIR/loop.sh" ]; then
    echo "Error: loop.sh not found in $SCRIPT_DIR"
    exit 1
fi

# Verify gh is authenticated
if ! gh auth status >/dev/null 2>&1; then
    echo "Error: GitHub CLI not authenticated. Run 'gh auth login' first."
    exit 1
fi

# Create log directory
mkdir -p "$LOG_DIR"

# Initialize state file
if [ ! -f "$STATE_FILE" ]; then
    echo "{}" > "$STATE_FILE"
fi

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_DIR/automation.log"
}

# Get processed issues from state file
get_processed_issues() {
    if [ -f "$STATE_FILE" ]; then
        cat "$STATE_FILE"
    else
        echo "{}"
    fi
}

# Mark issue as processed
mark_issue_processed() {
    local issue_number="$1"
    local state=$(get_processed_issues)
    echo "$state" | jq --arg num "$issue_number" --arg time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        '.[$num] = $time' > "$STATE_FILE"
}

# Check if issue was processed
is_issue_processed() {
    local issue_number="$1"
    local state=$(get_processed_issues)
    echo "$state" | jq -e --arg num "$issue_number" 'has($num)' >/dev/null 2>&1
}

# Process a single issue
process_issue() {
    local issue_number="$1"
    local issue_title="$2"
    local issue_body="$3"

    log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    log "Processing issue #$issue_number: $issue_title"
    log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    local issue_log_dir="$LOG_DIR/issue-$issue_number"
    mkdir -p "$issue_log_dir"

    # Save issue details
    echo "$issue_body" > "$issue_log_dir/issue-description.txt"

    # Define prompt file paths
    local prompt_plan_file="$SCRIPT_DIR/loop/PROMPT_plan.claude.md"
    local prompt_build_file="$SCRIPT_DIR/loop/PROMPT_build.claude.md"
    local backup_plan_file="$issue_log_dir/PROMPT_plan.backup"
    local backup_build_file="$issue_log_dir/PROMPT_build.backup"

    # Backup original prompt files
    if [ -f "$prompt_plan_file" ]; then
        cp "$prompt_plan_file" "$backup_plan_file"
    fi
    if [ -f "$prompt_build_file" ]; then
        cp "$prompt_build_file" "$backup_build_file"
    fi

    # Ensure cleanup on exit
    cleanup_prompts() {
        log "Restoring original prompt files..."
        if [ -f "$backup_plan_file" ]; then
            mv "$backup_plan_file" "$prompt_plan_file"
        fi
        if [ -f "$backup_build_file" ]; then
            mv "$backup_build_file" "$prompt_build_file"
        fi
    }
    trap cleanup_prompts EXIT

    # Create issue-specific prompt files
    cat > "$prompt_plan_file" <<EOF
# GitHub Issue #$issue_number: $issue_title

## Issue Description
$issue_body

## Task
Please analyze this GitHub issue and create a comprehensive plan to address it.
Think through the requirements carefully and break down the solution into clear steps.

EOF

    cat > "$prompt_build_file" <<EOF
# GitHub Issue #$issue_number: $issue_title

## Issue Description
$issue_body

## Task
Please implement the solution for this GitHub issue.
Follow the plan that was created and ensure all requirements are met.

EOF

    # Run loop.sh plan
    log "Running loop.sh plan for issue #$issue_number..."
    if cd "$SCRIPT_DIR" && ./loop.sh plan > "$issue_log_dir/plan.log" 2>&1; then
        log "✔ Plan completed successfully"
    else
        local exit_code=$?
        log "✖ Plan failed with exit code $exit_code"
        log "Check logs at: $issue_log_dir/plan.log"

        # Restore prompts before returning
        cleanup_prompts
        trap - EXIT

        # Comment on the issue about the failure
        gh issue comment "$issue_number" --repo "$REPO" --body \
            "❌ Automated processing failed during planning phase. Exit code: $exit_code

Please check the logs for details." || true

        return 1
    fi

    # Run loop.sh build
    log "Running loop.sh build for issue #$issue_number..."
    if cd "$SCRIPT_DIR" && ./loop.sh > "$issue_log_dir/build.log" 2>&1; then
        log "✔ Build completed successfully"
    else
        local exit_code=$?
        log "✖ Build failed with exit code $exit_code"
        log "Check logs at: $issue_log_dir/build.log"

        # Restore prompts before returning
        cleanup_prompts
        trap - EXIT

        # Comment on the issue about the failure
        gh issue comment "$issue_number" --repo "$REPO" --body \
            "❌ Automated processing failed during build phase. Exit code: $exit_code

Please check the logs for details." || true

        return 1
    fi

    # Restore original prompts
    cleanup_prompts
    trap - EXIT

    # Close the issue with a success comment
    log "Closing issue #$issue_number..."
    gh issue close "$issue_number" --repo "$REPO" --comment \
        "✅ This issue has been automatically processed and resolved.

The automated system has:
1. ✓ Created a plan to address the issue
2. ✓ Implemented the solution
3. ✓ Completed all required tasks

If this doesn't fully address your issue, please feel free to reopen it with additional details." || {
        log "✖ Failed to close issue #$issue_number"
        return 1
    }

    log "✔ Issue #$issue_number closed successfully"

    # Mark as processed
    mark_issue_processed "$issue_number"

    log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    return 0
}

# Main monitoring loop
log "Starting GitHub issue automation for $REPO"
log "Polling interval: ${POLL_INTERVAL}s"
log "State file: $STATE_FILE"
log "Log directory: $LOG_DIR"
log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

while true; do
    log "Checking for new or reopened issues..."

    # Get all open issues (new and reopened)
    issues=$(gh issue list --repo "$REPO" --state open --json number,title,body,createdAt,updatedAt --limit 100)

    if [ -z "$issues" ] || [ "$issues" = "[]" ]; then
        log "No open issues found"
    else
        # Process each issue
        echo "$issues" | jq -c '.[]' | while IFS= read -r issue; do
            issue_number=$(echo "$issue" | jq -r '.number')
            issue_title=$(echo "$issue" | jq -r '.title')
            issue_body=$(echo "$issue" | jq -r '.body // ""')

            # Check if we've already processed this issue
            if is_issue_processed "$issue_number"; then
                log "Issue #$issue_number already processed, skipping"
                continue
            fi

            log "Found new/reopened issue #$issue_number: $issue_title"

            # Process the issue
            if process_issue "$issue_number" "$issue_title" "$issue_body"; then
                log "✔ Successfully processed issue #$issue_number"
            else
                log "✖ Failed to process issue #$issue_number"
                # Mark as processed even if failed, to avoid infinite retry
                # User can reopen if needed
                mark_issue_processed "$issue_number"
            fi
        done
    fi

    log "Waiting ${POLL_INTERVAL}s before next check..."
    sleep "$POLL_INTERVAL"
done
