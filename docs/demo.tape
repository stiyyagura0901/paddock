# The README demo: paddock --dry-run in a fresh project.
#
# Re-render docs/demo.gif from the repository root:
#
#   docker run --rm -v "$PWD":/src -w /src -e CGO_ENABLED=0 golang:1.24 \
#     go build -o /src/docs/.demo-bin/paddock ./cmd/paddock
#   docker run --rm -v "$PWD/docs":/vhs ghcr.io/charmbracelet/vhs:v0.10.0 demo.tape
#   rm -rf docs/.demo-bin
#
# docs/demo-setup.sh builds a fake home (/home/dev) with a sample ~/.claude
# and paddock config, so nothing from your own machine appears.

Output demo.gif

Set Shell "bash"
Set Theme "Catppuccin Mocha"
Set FontSize 16
Set Width 1100
Set Height 620
Set Padding 20
Set TypingSpeed 50ms

Hide
Type "source /vhs/demo-setup.sh && clear"
Enter
Sleep 500ms
Show

Sleep 500ms
Type "cd ~/code/my-app"
Sleep 300ms
Enter
Sleep 600ms
Type "paddock --dry-run"
Sleep 400ms
Enter
Sleep 4s
