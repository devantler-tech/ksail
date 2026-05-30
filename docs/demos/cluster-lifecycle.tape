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
Set TypingSpeed 50ms
Set PlaybackSpeed 1.5
Set Framerate 24

Sleep 800ms
Type "# 1. Create a real Kubernetes cluster (Kind on Docker)"
Sleep 300ms
Enter
Sleep 500ms
Type "ksail cluster init --name ksail-demo"
Sleep 200ms
Enter
Sleep 1500ms
Type "ksail cluster create"
Sleep 200ms
Enter
Sleep 2500ms

Type "# 2. Inspect the running cluster"
Sleep 300ms
Enter
Sleep 500ms
Type "ksail cluster info"
Sleep 200ms
Enter
Sleep 5000ms

Type "# 3. Deploy a workload"
Sleep 300ms
Enter
Sleep 500ms
Type "ksail workload create deployment web --image=nginx:alpine --replicas=2"
Sleep 200ms
Enter
Sleep 1500ms
Type "ksail workload rollout status deployment/web"
Sleep 200ms
Enter
Sleep 1500ms
Type "ksail workload get pods"
Sleep 200ms
Enter
Sleep 4500ms

Type "# 4. Tear it all down (ephemeral by design)"
Sleep 300ms
Enter
Sleep 500ms
Type "ksail cluster delete --name ksail-demo --force"
Sleep 200ms
Enter
Sleep 6000ms
