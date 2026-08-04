# Create and store a PPIO API key

[简体中文](../zh/02-api-key.md) · **English**

## Before you create one

Confirm that:

- You are logged in to the right PPIO account.
- The account has usable credit or an allowance.
- You know which machine and which project this key is for.

## Steps

1. Open the API key page in the PPIO console.
2. Create a new API key.
3. Copy it the moment it is first shown.
4. Paste it into OneAgent's password field.
5. Click **Test connection**.

The key is shown only once. Do not leave the page expecting to read it again later.

## How OneAgent handles the key

OneAgent's standing constraints:

- The key travels from the local form to the local install flow, and nowhere else.
- The key is never passed as a command-line argument.
- The key is never written to logs or error messages.
- The key is never uploaded to a OneAgent server.
- A timestamped backup is created before any configuration is overwritten.

## Recommended local environment variables

```bash
export ONEAGENT_API_KEY='your PPIO API key'
export ONEAGENT_API_BASE_URL='https://api.ppio.com/openai'
export ONEAGENT_MODEL='your model ID'
```

If you use CC Switch, put the key only in CC Switch's local profile, and follow the local
storage and permission guidance for the version you have installed.

## If a key leaks

Revoke the old key in the PPIO console immediately, create a new one, and run OneAgent's
setup again. Deleting the chat message or the git commit is not enough — the key may
already sit in a cache, a log, or a screenshot.

## Official references

- PPIO API keys: <https://resource.ppio.com/docs/support/api-key>
- PPIO API integration: <https://ppio.com/docs/model/inference>
