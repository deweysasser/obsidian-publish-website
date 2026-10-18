package render

import (
	"path"
	"sort"
	"strings"
)

// FolderListing returns markdown listing a published folder's subfolders and
// pages. Used for the folder-index-content block and generated folder pages.
func (r *Renderer) FolderListing(folder string) string {
	type entry struct{ name, url string }
	var folders, pages []entry
	for f, slug := range r.Pub.Folders {
		if f != folder && path.Dir(f) == folder {
			folders = append(folders, entry{path.Base(f), r.URLs.PageURL(slug)})
		}
	}
	for p, slug := range r.Pub.Pages {
		if path.Dir(p) == folder && r.Pub.FolderNote(folder) != p && slug != "" {
			pages = append(pages, entry{strings.TrimSuffix(path.Base(p), path.Ext(p)), r.URLs.PageURL(slug)})
		}
	}
	less := func(es []entry) func(i, j int) bool {
		return func(i, j int) bool { return strings.ToLower(es[i].name) < strings.ToLower(es[j].name) }
	}
	sort.Slice(folders, less(folders))
	sort.Slice(pages, less(pages))
	var b strings.Builder
	for _, e := range folders {
		b.WriteString("- 📁 [" + mdEscaper.Replace(e.name) + "](" + e.url + ")\n")
	}
	for _, e := range pages {
		b.WriteString("- [" + mdEscaper.Replace(e.name) + "](" + e.url + ")\n")
	}
	return b.String()
}

func (r *Renderer) folderIndexHook(_ string, _ int, ctx Context) (string, error) {
	return r.FolderListing(path.Dir(ctx.Path)), nil
}
