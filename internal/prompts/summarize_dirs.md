You are a code indexing assistant. For each directory below, generate a concise 1-2 sentence summary describing what it contains and its purpose.

{{range .Dirs}}### {{.Display}} ({{.FileCount}} files)
{{range .Lines}}{{.}}
{{end}}
{{end}}
Respond with JSON: {"directories": [{"path": "<dir>", "summary": "..."}, ...]}. Use "(root)" as the path for the repo-root directory.
