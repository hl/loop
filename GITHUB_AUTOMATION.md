# GitHub Issue Automation

This script automates the processing of GitHub issues by monitoring a repository and automatically running the loop.sh workflow when new or reopened issues are detected.

## Overview

The `github-issue-automation.sh` script:
1. Monitors a GitHub repository for new or reopened issues
2. When detected, automatically runs `loop.sh plan` to create a solution plan
3. Then runs `loop.sh build` to implement the solution
4. Closes the issue with a success message
5. Continues monitoring for new issues

## Prerequisites

1. **GitHub CLI (gh)**: Install from https://cli.github.com/
   ```bash
   # Verify installation
   gh --version
   ```

2. **GitHub Authentication**: Authenticate the CLI
   ```bash
   gh auth login
   ```

3. **loop.sh**: The script must be in the same directory as `loop.sh`

4. **jq**: JSON processor (usually pre-installed on most systems)
   ```bash
   # Verify installation
   jq --version
   ```

## Usage

```bash
./github-issue-automation.sh <owner/repo> [--poll-interval SECONDS]
```

### Parameters

- `<owner/repo>`: **Required**. The GitHub repository to monitor (e.g., `myorg/myproject`)
- `--poll-interval SECONDS`: Optional. How often to check for new issues (default: 60 seconds)

### Examples

Monitor your repository with default settings (60-second polling):
```bash
./github-issue-automation.sh myorg/myproject
```

Monitor with a 30-second polling interval:
```bash
./github-issue-automation.sh myorg/myproject --poll-interval 30
```

Monitor a different repository:
```bash
./github-issue-automation.sh octocat/hello-world --poll-interval 120
```

## How It Works

### Issue Detection

The script maintains a state file (`.github-issue-state`) that tracks which issues have been processed. Each polling cycle:
- Fetches all open issues from the repository
- Compares against the state file
- Processes any new or reopened issues

### Processing Workflow

For each new/reopened issue:

1. **Backup Original Prompts**: Saves current `PROMPT_plan.claude.md` and `PROMPT_build.claude.md`

2. **Create Issue-Specific Prompts**: Generates temporary prompts containing:
   - Issue number and title
   - Full issue description
   - Task instructions

3. **Run Planning Phase**: Executes `loop.sh plan`
   - Creates a solution plan based on the issue
   - Logs output to `loop/logs/github-automation/issue-N/plan.log`

4. **Run Build Phase**: Executes `loop.sh build`
   - Implements the solution
   - Logs output to `loop/logs/github-automation/issue-N/build.log`

5. **Restore Prompts**: Returns original prompt files to their state

6. **Close Issue**: Posts a success comment and closes the issue

7. **Mark as Processed**: Updates state file to prevent reprocessing

### Error Handling

If any phase fails:
- The error is logged
- A comment is posted to the issue explaining the failure
- Original prompts are restored
- The issue is marked as processed (to avoid infinite retry loops)
- The user can reopen the issue if needed

### State Management

- **State File**: `.github-issue-state` (JSON format)
- Contains a mapping of issue numbers to processing timestamps
- Prevents duplicate processing of the same issue
- Persists across script restarts

### Logging

All activity is logged to:
- **Main log**: `loop/logs/github-automation/automation.log`
- **Issue-specific logs**: `loop/logs/github-automation/issue-N/`
  - `plan.log`: Output from planning phase
  - `build.log`: Output from build phase
  - `issue-description.txt`: Original issue content

## Running as a Service

For continuous monitoring, you can run the script as a background service:

### Using nohup

```bash
nohup ./github-issue-automation.sh myorg/myproject > automation.out 2>&1 &
```

### Using systemd (Linux)

Create `/etc/systemd/system/github-issue-automation.service`:

```ini
[Unit]
Description=GitHub Issue Automation
After=network.target

[Service]
Type=simple
User=youruser
WorkingDirectory=/path/to/loop
ExecStart=/path/to/loop/github-issue-automation.sh myorg/myproject
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Then:
```bash
sudo systemctl daemon-reload
sudo systemctl enable github-issue-automation
sudo systemctl start github-issue-automation
```

### Using screen or tmux

```bash
# Using screen
screen -S github-automation
./github-issue-automation.sh myorg/myproject
# Press Ctrl+A, then D to detach

# Using tmux
tmux new -s github-automation
./github-issue-automation.sh myorg/myproject
# Press Ctrl+B, then D to detach
```

## Configuration

The script uses the same configuration as `loop.sh`:
- **Config file**: `loop/config.ini`
- **Prompt files**: `loop/PROMPT_plan.claude.md` and `loop/PROMPT_build.claude.md`
- These are temporarily modified during issue processing and restored afterward

## Troubleshooting

### Script won't start

1. Check GitHub CLI authentication:
   ```bash
   gh auth status
   ```

2. Verify repository access:
   ```bash
   gh issue list --repo myorg/myproject
   ```

3. Check that `loop.sh` exists and is executable:
   ```bash
   ls -l loop.sh
   ```

### Issues not being processed

1. Check the automation log:
   ```bash
   tail -f loop/logs/github-automation/automation.log
   ```

2. Verify issues are actually open:
   ```bash
   gh issue list --repo myorg/myproject --state open
   ```

3. Check the state file:
   ```bash
   cat .github-issue-state
   ```

### Processing failures

1. Review issue-specific logs:
   ```bash
   cat loop/logs/github-automation/issue-N/plan.log
   cat loop/logs/github-automation/issue-N/build.log
   ```

2. Check that original prompts are restored:
   ```bash
   ls -l loop/PROMPT_*.claude.md
   ```

## Security Considerations

- The script has full access to your repository through the GitHub CLI
- It will automatically close issues, so ensure proper access controls
- Consider running in a sandboxed environment for untrusted repositories
- Review logs regularly to ensure expected behavior

## Limitations

- Processes issues sequentially (one at a time)
- Requires continuous operation to monitor for new issues
- Polling-based (not real-time webhook-based)
- No retry mechanism for failed issues (marked as processed on failure)

## Future Enhancements

Potential improvements:
- Webhook-based triggering instead of polling
- Parallel processing of multiple issues
- Configurable retry logic for failed issues
- Integration with GitHub Actions
- Support for issue labels and filters
- Custom success/failure notification templates
