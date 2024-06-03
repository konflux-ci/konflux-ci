Contributing Guidelines
===

<!-- toc -->

- [Editing Markdown Files](#editing-markdown-files)

<!-- tocstop -->

# Editing Markdown Files

If the structure of markdown files containing table of contents changes, those
need to be updated as well.

To do that, run the command below and add the produced changes to your PR.

```bash
find . -name "*.md" | while read -r file; do
    npx markdown-toc $file -i
done
```
