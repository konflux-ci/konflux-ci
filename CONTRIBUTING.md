Contributing Guidelines
===

<!-- toc -->

- [Editing Markdown Files](#editing-markdown-files)
- [Using KubeLinter](#using-kubelinter)

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

# Using KubeLinter

Please consider running [KubeLinter](https://docs.kubelinter.io/#/?id=usage) 
locally before submitting a PR to this repository.

After [installing KubeLinter](https://docs.kubelinter.io/#/?id=installing-kubelinter)
and adding it to the $PATH env variable, enter the root directory of Konflux-ci
and run `kube-linter lint .` to recursively apply KubeLinter checks on this folder.

It may be also recommended to create a configuration file, to do so please check
[KubeLinter config documentation](https://docs.kubelinter.io/#/configuring-kubelinter)
this file will allow you to ignore or include specific KubeLInter checks.
