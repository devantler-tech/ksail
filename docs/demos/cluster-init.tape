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
Set TypingSpeed 45ms

Sleep 800ms
Type "# 1. Scaffold a GitOps-ready cluster project"
Sleep 300ms
Enter
Type "ksail cluster init"
Sleep 300ms
Enter
Sleep 2000ms

Type "# 2. The whole cluster, declaratively"
Sleep 300ms
Enter
Type "cat ksail.yaml"
Enter
Sleep 2200ms

Type "# 3. Switch distribution in one command"
Sleep 300ms
Enter
Type "ksail cluster init --distribution K3s --force"
Sleep 300ms
Enter
Sleep 2000ms
Type "cat ksail.yaml"
Enter
Sleep 2500ms

Type "# Ready to launch: ksail cluster create"
Sleep 300ms
Enter
Sleep 2200ms
