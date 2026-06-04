# Full lifecycle on a REAL Kind cluster: create, inspect, deploy, tear down.
# Requires Docker. Rendered via ./render.sh (runs in a scratch dir). The demo
# names its cluster "ksail-demo" and deletes it at the end; render.sh also has a
# --force safety-net trap that removes a leftover ksail-demo cluster on abort.
Output "cluster-lifecycle.gif"
Output "cluster-lifecycle.mp4"
Require ksail

Set Shell "bash"
Set FontSize 20
Set Width 1200
Set Height 700
Set Padding 25
Set Margin 24
Set MarginFill "#1e1e2e"
Set BorderRadius 10
Set WindowBar Colorful
Set Theme "Catppuccin Mocha"
Set TypingSpeed 60ms
Set Framerate 24
# A mild 1.25x speed-up trims the long, real-time `cluster create` without making
# the readable parts feel rushed (the old 1.5x did). TypingSpeed is bumped to 60ms
# so that, after the speed-up, typed commands still read at a natural ~48ms/char;
# reading `Sleep`s below are likewise sized for their post-speed-up duration.
Set PlaybackSpeed 1.25
# Wait for each command to finish (prompt returns) before typing the next line, so
# the streaming output of `cluster create` / `rollout status` never collides with
# the next command. Timeout covers a cold `cluster create` pulling images.
Set WaitTimeout 180s

Sleep 1s
Type "# 1. Create a real Kubernetes cluster (Kind on Docker)"
Sleep 700ms
Enter
Type "ksail cluster init --name ksail-demo"
Sleep 400ms
Enter
Wait
Sleep 1500ms
Type "ksail cluster create"
Sleep 400ms
Enter
Wait
Sleep 3s

Type "# 2. Inspect the running cluster"
Sleep 700ms
Enter
Type "ksail cluster info"
Sleep 400ms
Enter
Wait
Sleep 5200ms

Type "# 3. Deploy a workload"
Sleep 700ms
Enter
Type "ksail workload create deployment web --image=nginx:alpine --replicas=2"
Sleep 400ms
Enter
Wait
Sleep 2s
Type "ksail workload rollout status deployment/web"
Sleep 400ms
Enter
Wait
Sleep 2s
Type "ksail workload get pods"
Sleep 400ms
Enter
Wait
Sleep 4s

Type "# 4. Tear it all down (ephemeral by design)"
Sleep 700ms
Enter
Type "ksail cluster delete --name ksail-demo --force"
Sleep 400ms
Enter
Wait
Sleep 3500ms
