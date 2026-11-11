# Host context and MCP capabilities

## ChatGPT through MCP

A remote MCP server sees its protocol requests and responses. That surface
does not expose the entire conversation, hidden system instructions,
reasoning tokens, exact model window usage or all other tool results.

A checkpoint operation cannot be invoked retroactively after a host loses
its working context. An assistant must invoke it while useful evidence and
decisions are still accessible. An MCP server cannot create a new ChatGPT
chat on behalf of a user or silently transfer active conversation control.

## Model host accounting

The OpenAI Responses API returns model-token usage for API calls. The
API also provides a token counting endpoint and host-managed context
compaction. These measurements and operations are available to the API
client that owns the response chain, not automatically to a remote MCP.

A host adapter may attach measured usage and choose to compact or start
a fresh context. It must preserve durable project evidence separately
from any model-managed opaque compaction state. The adapter must not
send hidden prompts, credentials or private transcript contents to
the MCP's checkpoint store.

Relevant product references:

- https://developers.openai.com/api/docs/guides/compaction
- https://developers.openai.com/api/docs/guides/conversation-state
- https://developers.openai.com/api/reference/resources/responses/subresources/input_tokens/methods/count
