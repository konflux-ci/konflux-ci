---
title: "LLM-Friendly Documentation (llms.txt)"
linkTitle: "LLM-Friendly Docs"
weight: 11
description: "How to use the Konflux Operator llms.txt file with MCP servers so AI coding assistants can access the documentation."
---

The Konflux Operator documentation publishes an [`llms.txt`](https://llmstxt.org/)
file — a structured index that lets Large Language Models discover and consume the
docs at inference time. The file is automatically generated from the Hugo content
tree and stays in sync with documentation changes.

**Published at:** <https://konflux-ci.dev/konflux-ci/llms.txt>

## What is llms.txt?

`llms.txt` is a lightweight specification for providing LLM-readable content on
websites. It is a Markdown file with a title, description, and a list of links to
documentation pages. AI coding assistants and MCP-enabled IDEs can read this file
to find relevant documentation without scraping HTML.

## Using with an MCP server (mcpdoc)

[mcpdoc](https://github.com/langchain-ai/mcpdoc) is an open-source MCP server
purpose-built for serving `llms.txt` files. It exposes two tools to MCP host
applications (Cursor, Windsurf, Claude Code/Desktop):

- **`list_doc_sources`** — lists available documentation sources
- **`fetch_docs`** — fetches content from URLs found in the `llms.txt` file

### Konflux llms.txt URL

Use the following name and URL when configuring mcpdoc:

| Name | URL |
|------|-----|
| `Konflux Operator And Administrator Documentation` | `https://konflux-ci.dev/konflux-ci/llms.txt` |

### Quick start (Cursor example)

Open **Cursor Settings > MCP** to edit your `~/.cursor/mcp.json`, then add:

```json
{
  "mcpServers": {
    "konflux-operator-docs": {
      "command": "uvx",
      "args": [
        "--from",
        "mcpdoc",
        "mcpdoc",
        "--urls",
        "Konflux Operator And Administrator Documentation:https://konflux-ci.dev/konflux-ci/llms.txt",
        "--transport",
        "stdio"
      ]
    }
  }
}
```

For best results, add a rule to your Cursor **User Rules** (Settings > Rules):

```text
For ANY question about Konflux, the Konflux Operator, or Konflux administration,
use the konflux-operator-docs MCP server:
1. Call list_doc_sources to see available llms.txt files
2. Call fetch_docs to read the llms.txt index
3. Identify URLs relevant to the question
4. Call fetch_docs on those URLs
5. Use the retrieved content to answer
```

### Other IDEs and clients

mcpdoc supports Cursor, Windsurf, Claude Code, and Claude Desktop. For setup
instructions for each client, see the
[mcpdoc Quickstart guide](https://github.com/langchain-ai/mcpdoc?tab=readme-ov-file#quickstart).
The configuration is the same across clients — only the `--urls` value is
Konflux-specific:

```text
Konflux Operator And Administrator Documentation:https://konflux-ci.dev/konflux-ci/llms.txt
```

### Testing with the MCP Inspector

Start mcpdoc in SSE mode:

```bash
uvx --from mcpdoc mcpdoc \
    --urls "Konflux Operator And Administrator Documentation:https://konflux-ci.dev/konflux-ci/llms.txt" \
    --transport sse \
    --port 8082 \
    --host localhost
```

Then launch the [MCP Inspector](https://modelcontextprotocol.io/docs/tools/inspector):

```bash
npx @modelcontextprotocol/inspector
```

In the Inspector UI, set the transport to **SSE**, enter `http://localhost:8082/sse`,
and click **Connect**. Use the **Tools** tab to call `list_doc_sources` and
`fetch_docs`.

### Testing with a local docs server

When developing documentation locally, point mcpdoc at the local Hugo server
instead of the production URL:

```bash
make docs-serve

# In another terminal
uvx --from mcpdoc mcpdoc \
    --urls "Konflux Operator And Administrator Documentation:http://localhost:4000/konflux-ci/operator/llms.txt" \
    --allowed-domains localhost raw.githubusercontent.com \
    --transport sse \
    --port 8082 \
    --host localhost
```

{{< alert color="info" >}}
When using a local `llms.txt` URL, you must pass `--allowed-domains` explicitly
because mcpdoc only auto-allows the domain of the `llms.txt` URL itself. The
documentation links inside `llms.txt` point to `raw.githubusercontent.com`, so
that domain must be allowed as well.
{{< /alert >}}

## How llms.txt is generated

The file is produced by `hack/generate-llms-txt.sh`, which scans the Hugo content
tree and extracts title, description, and weight from each page's YAML frontmatter.
It runs automatically as part of `make generate-docs`, so any changes to the
documentation are reflected in `llms.txt` on the next build.
