package office

import "testing"

func TestImportFormatsDoNotPretendToSaveLegacyFiles(t *testing.T) {
	for _, item := range []struct {
		ext, kind, target string
		edit              bool
	}{{"docx", "word", "docx", true}, {"xlsx", "cell", "xlsx", true}, {"pptx", "slide", "pptx", true}, {"pdf", "pdf", "docx", true}, {"DOC", "word", "docx", false}, {"odt", "word", "docx", false}, {"rtf", "word", "docx", false}, {"xls", "cell", "xlsx", false}, {"ods", "cell", "xlsx", false}, {"csv", "cell", "xlsx", false}, {"ppt", "slide", "pptx", false}, {"odp", "slide", "pptx", false}, {"exe", "", "", false}} {
		kind, target, edit := Format(item.ext)
		if kind != item.kind || target != item.target || edit != item.edit {
			t.Fatalf("%s: %s %s %v", item.ext, kind, target, edit)
		}
	}
}
