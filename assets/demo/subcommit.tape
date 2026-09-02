Output assets/demo/subcommit.gif
Output assets/demo/subcommit.mp4

Set Shell "bash"
Set Width 1300
Set Height 1020
Set FontSize 15
Set LineHeight 1.0
Set TypingSpeed 35ms
Set Framerate 24
Set Theme { "name": "GitHub Dark Neutral", "black": "#0d1117", "red": "#ff7b72", "green": "#7ee787", "yellow": "#d29922", "blue": "#79c0ff", "magenta": "#d2a8ff", "cyan": "#a5d6ff", "white": "#c9d1d9", "brightBlack": "#8b949e", "brightRed": "#ffa198", "brightGreen": "#aff5b4", "brightYellow": "#e3b341", "brightBlue": "#a5d6ff", "brightMagenta": "#d2a8ff", "brightCyan": "#b6e3ff", "brightWhite": "#f0f6fc", "background": "#0d1117", "foreground": "#c9d1d9", "selection": "#264f78", "cursor": "#f0f6fc" }
Set Padding 20
Set Margin 16
Set MarginFill "#010409"
Set WindowBar Colorful
Set BorderRadius 8
Set CursorBlink false

Env PS1 "\n$ "
Env PS0 "\n"
Env GIT_AUTHOR_DATE "2000-01-01T00:00:00Z"
Env GIT_COMMITTER_DATE "2000-01-01T00:00:00Z"

Hide
Type `export PATH="$PWD/assets/demo/bin:$PATH"`
Enter
Type `export DEMO_DIR="${TMPDIR:-/tmp}/subcommit-demo"`
Enter
Type `rm -rf "$DEMO_DIR" && git init -q -b main "$DEMO_DIR" && cd "$DEMO_DIR"`
Enter
Type "git config user.name Demo && git config user.email demo@example.test && git config commit.gpgsign false && git config core.autocrlf false"
Enter
Type `mkdir hooks && git config core.hooksPath "$DEMO_DIR/hooks"`
Enter
Type `printf '%s\n' '[app]' name=subcommit greeting=hello mode=safe '' '[display]' theme=dark color=blue '' timeout=30 retries=3 '' '[logging]' level=info > app.conf`
Enter
Type "git add app.conf && git commit -qm seed"
Enter
Type `printf '%s\n' '[app]' name=subcommit greeting=hello-world mode=safe '' '[display]' theme=dark color=blue '' timeout=60 retries=3 '' '[logging]' level=debug > app.conf`
Enter
Type "printf 'release notes\n' > release.txt"
Enter
Type "printf 'staged work\n' > staged.txt && git add staged.txt"
Enter
Type "printf 'draft notes\n' > notes.txt"
Enter
Type "clear"
Enter
Show

Type "git status --short"
Enter
Sleep 2s

Type `subcommit --yes \`
Enter
Type `  -m "prepare release" \`
Enter
Type `  release.txt \`
Enter
Type "  app.conf:2-4,10"
Sleep 1s
Enter
Sleep 3s

Type "git status --short"
Enter
Sleep 4s

Hide
Type `cd / && rm -rf "$DEMO_DIR"`
Enter
