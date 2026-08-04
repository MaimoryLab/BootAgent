# Verify your first request

[简体中文](../zh/05-first-request.md) · **English**

## 1. Check the model list first

```bash
curl https://api.ppio.com/openai/v1/models \
  -H "Authorization: Bearer $ONEAGENT_API_KEY"
```

Confirm the model ID you intend to use appears in the response.

## 2. Verify with a minimal chat request

```bash
curl https://api.ppio.com/openai/v1/chat/completions \
  -H "Authorization: Bearer $ONEAGENT_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "YOUR_MODEL_ID",
    "messages": [{"role": "user", "content": "Reply with OK."}],
    "max_tokens": 8
  }'
```

Do not use real project code, personal information, or a long prompt as your first test.

## 3. Common status codes

| Status | Meaning | What to do |
| --- | --- | --- |
| 200 | Request succeeded | Go ahead and start the agent |
| 401 / 403 | Key rejected, or no permission | Create a new key; check it was pasted in full |
| 404 / 405 | Address or endpoint unsupported | Check the base URL has no duplicated `/v1` or full path |
| 429 | Rate or quota limit | Review your account credit and request rate |
| 500 | Server error | Note the time and status code, retry later |

## 4. Verify through the agent

Start the agent and try a small task:

```text
Read the README in the current directory and list three things that could be improved.
Do not modify any files.
```

If the agent returns a result, the install, key, model, and base configuration are all
working.

## Official references

- PPIO model overview: <https://ppio.com/docs/model/overview>
- PPIO API integration: <https://ppio.com/docs/model/inference>
