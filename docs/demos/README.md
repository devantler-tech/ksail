# Documentation demos

Scripted terminal recordings used in the docs, built with [VHS](https://github.com/charmbracelet/vhs).

Each `*.tape` is a VHS script that runs **real `ksail` commands**. Rendering produces a GIF and an
MP4 in [`../public/demos/`](../public/demos/) (served at `/demos/<name>.{gif,mp4}`), which are
committed so the published site serves them without a build-time render step.

| Tape                     | Demo                                                                              | Docker? |
|--------------------------|-----------------------------------------------------------------------------------|---------|
| `cluster-init.tape`      | Quick start: `cluster init`, the declarative `ksail.yaml`, switching distribution | no      |
| `cluster-lifecycle.tape` | Full lifecycle on a real Kind cluster: create → info → deploy → delete            | **yes** |

## Regenerate

```bash
brew install vhs          # one-time; pulls ttyd + ffmpeg
brew install gifsicle     # one-time; optional, shrinks the GIFs

./render.sh               # render every *.tape
./render.sh cluster-init  # render a single tape
```

`render.sh` builds `ksail` into `.bin/` (git-ignored), warms its page cache, then renders each tape
**inside a throwaway scratch directory** and moves the output into `../public/demos/`. Large GIFs are
downscaled with `gifsicle`; the MP4 keeps full resolution.

`cluster-lifecycle.tape` creates a real cluster named `ksail-demo` and deletes it at the end;
`render.sh` also has a `--force` safety-net trap that removes a leftover `ksail-demo` cluster if a
render aborts. Don't run it on a machine where that name is in use.

## Embedding in docs

Pick the format per page — GIF for short clips (autoplays in plain markdown and on GitHub), MP4
`<video>` for longer ones (smaller, scrubbable). Both share the `.demo-recording` class from
`src/styles/custom.css`:

```mdx
<!-- short clip -->
<img class="demo-recording" src="/demos/cluster-init.gif" alt="…" />

<!-- longer clip, with the GIF as a fallback -->
<video class="demo-recording" autoplay loop muted playsinline controls aria-label="…">
  <source src="/demos/cluster-lifecycle.mp4" type="video/mp4" />
  <img class="demo-recording" src="/demos/cluster-lifecycle.gif" alt="…" />
</video>
```

## Authoring tapes — gotchas

These are baked into the existing tapes; keep them in mind when adding new ones.

- **No on-screen setup.** VHS's `Hide`/`Show` is broken in 0.11.0 (hidden commands still render), and
  VHS launches bash with `--norc` and injects its own `>` prompt (so `~/.bashrc`/`PS1` overrides are
  ignored). The workaround is structural: `render.sh` runs the tape from a clean scratch dir, so demo
  commands start from a blank project with nothing to `cd`/`source`/`clear` on camera.
- **ASCII only in `Type`.** An em-dash (`—`) and other non-ASCII corrupt VHS typing — it drops/reorders
  characters, and a dropped leading `#` turns a comment into a command. Keep typed lines plain ASCII.
- **Wait for commands to finish — don't guess with `Sleep`.** A slow command (`cluster create`,
  `rollout status`) whose output is still streaming will collide with the next `Type`, interleaving
  the typed line into the command's output. Put a bare `Wait` after the command's `Enter`: it blocks
  until the shell prompt returns (VHS matches the prompt on the last line, so mid-command streaming
  never triggers it). Pair it with `Set WaitTimeout` long enough for the slowest command (e.g. `180s`
  to cover a cold `cluster create` that pulls images). Use `Sleep` only for *readability* pauses —
  holding finished output on screen long enough to read — never as a substitute for `Wait`.
- **Speed up playback gently, and compensate.** `Set PlaybackSpeed` scales the *whole* recording —
  typing, reading pauses, and command output alike. A mild value (≤ 1.25) is useful to trim long
  real-time command waits (e.g. `cluster create`), but anything aggressive (the old 1.5) makes the
  readable parts flash by. If you do speed up, size `TypingSpeed` and the reading `Sleep`s for their
  *post-speed-up* duration — e.g. at 1.25 a `Sleep 5200ms` reads as ~4.2 s — so typing and pauses
  stay comfortable. `cluster-init` needs no speed-up (no slow commands); only `cluster-lifecycle` does.
- **`Output` paths are relative** to the scratch dir — use a bare filename (`Output "name.gif"`);
  `render.sh` moves it to `../public/demos/`.
- **Destructive commands prompt.** `ksail cluster delete` asks for confirmation; use `--force` in tapes
  (typing `yes` races the prompt and can fall through to the shell's `yes` command).
