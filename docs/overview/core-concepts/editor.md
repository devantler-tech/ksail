# Editor

KSail-Go opens your preferred editor for interactive commands such as `ksail cipher edit` or when viewing generated manifests. Configure the editor with `ksail cluster init --editor` or by setting `spec.runtime.editor` in `ksail.yaml`.

## Nano

[Nano](https://www.nano-editor.org/) is the default because it is available on most systems and provides straightforward key bindings. Choose Nano if you want a no-frills editor for quick edits inside the CLI.

## Vim

[Vim](https://www.vim.org/) offers modal editing and extensive customization. Pick Vim when you already rely on its workflow or prefer its key bindings for editing secrets and manifests.
