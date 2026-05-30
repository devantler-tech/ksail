# Quick-start: scaffold a project and explore the declarative config.
# Fast, no Docker required. Rendered via ./render.sh (runs in a scratch dir, so
# there is no setup to type and `ksail cluster init` starts from a clean slate).
Output "cluster-init.gif"
Output "cluster-init.mp4"
Require ksail

Set Shell "bash"
Set FontSize 20
Set Width 1200
Set Height 720
Set Padding 25
Set Margin 24
Set MarginFill "#1e1e2e"
Set BorderRadius 10
Set WindowBar Colorful
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms
# Wait for each command's prompt to return before typing the next line, so output
# never collides with the following command. Generous timeout for a cold binary.
Set WaitTimeout 30s

Sleep 1s
Type "# 1. Scaffold a GitOps-ready cluster project"
Sleep 700ms
Enter
Type "ksail cluster init"
Sleep 400ms
Enter
Wait
Sleep 2800ms

Type "# 2. The whole cluster, declaratively"
Sleep 700ms
Enter
Type "cat ksail.yaml"
Sleep 400ms
Enter
Wait
Sleep 3s

Type "# 3. Switch distribution in one command"
Sleep 700ms
Enter
Type "ksail cluster init --distribution K3s --force"
Sleep 400ms
Enter
Wait
Sleep 2800ms
Type "cat ksail.yaml"
Sleep 400ms
Enter
Wait
Sleep 3500ms

Type "# Ready to launch: ksail cluster create"
Sleep 700ms
Enter
Sleep 3s
