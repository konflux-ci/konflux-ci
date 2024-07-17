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
and adding it to the $PATH env variable, create a new folder in the base directory 
using `mkdir -p ./.kube-linter/`. Then, run the following Bash script:
```
    find . -name "kustomization.yaml" -o -name "kustomization.yml" | while read -r file; do
    dir=$(dirname "$file")
    dir=${dir#./}
    output_file=$(echo "out-$dir" | tr "/" "-")
    kustomize build "$dir" > "./.kube-linter/$output_file.yaml"
    done
```
finally, run `kube-linter lint ./.kube-linter` to recursively apply KubeLinter checks on this folder.

It may be also recommended to create a configuration file. To do so please check
[KubeLinter config documentation](https://docs.kubelinter.io/#/configuring-kubelinter)
this file will allow you to ignore or include specific KubeLinter checks.
