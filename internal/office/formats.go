package office

import "strings"

// Format groups conversion targets. Editing/viewing permissions come from
// the installed Collabora server's discovery actions, including native formats.
func Format(ext string) (kind, target string, editable bool) {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	switch ext {
	case "docx":
		return "word", "docx", true
	case "xlsx":
		return "cell", "xlsx", true
	case "pptx":
		return "slide", "pptx", true
	case "pdf":
		return "pdf", "docx", true
	case "doc", "odt", "rtf", "txt", "md", "html", "htm", "epub", "dot", "dotx", "pages":
		return "word", "docx", false
	case "xls", "ods", "csv", "tsv", "xlt", "xltx", "numbers":
		return "cell", "xlsx", false
	case "ppt", "odp", "pps", "ppsx", "pot", "potx", "key":
		return "slide", "pptx", false
	}
	return "", "", false
}
