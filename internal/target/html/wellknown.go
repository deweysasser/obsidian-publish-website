package html

import (
	"fmt"
	"html/template"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
)

// writeSecurityTxt publishes site.security_txt at /.well-known/security.txt
// (RFC 9116) and, for scanners that still look there, /security.txt.
// Expires is a whole day, so rebuilds on the same day leave the file alone
// and an incremental deploy doesn't upload it again.
func (t *Target) writeSecurityTxt(st *config.SecurityTxt) error {
	if st == nil {
		return nil
	}
	days := st.ExpiresDays
	if days == 0 {
		days = config.DefaultSecurityTxtDays
	}
	var b strings.Builder
	for _, c := range st.Contact {
		fmt.Fprintf(&b, "Contact: %s\n", c)
	}
	y, m, d := t.now().UTC().Date()
	fmt.Fprintf(&b, "Expires: %s\n", time.Date(y, m, d+days, 0, 0, 0, 0, time.UTC).Format(time.RFC3339))
	if st.Policy != "" {
		fmt.Fprintf(&b, "Policy: %s\n", st.Policy)
	}
	if st.PreferredLanguages != "" {
		fmt.Fprintf(&b, "Preferred-Languages: %s\n", st.PreferredLanguages)
	}
	if t.origin != "" {
		fmt.Fprintf(&b, "Canonical: %s/.well-known/security.txt\n", t.origin)
	}
	for _, rel := range []string{".well-known/security.txt", "security.txt"} {
		if err := target.WriteFile(t.out, rel, []byte(b.String())); err != nil {
			return err
		}
	}
	return nil
}

// redirectKey is the frontmatter key listing a note's old URLs.
const redirectKey = "redirect_from"

// writeRedirects writes a small page at each old URL a note lists under
// redirect_from, sending visitors and search engines on to the note. Static
// hosts such as R2 can't answer with a real redirect, so this is a
// meta refresh with a canonical link. It runs after every other file is
// written and never replaces one: a path already taken is a warning.
//
// site.redirects entries go the same way, after every note's, so a note's
// own redirect_from wins a clash.
func (t *Target) writeRedirects(s *target.Site) error {
	claimed := map[string]string{} // output file → what redirects there
	// redirect writes one redirect page; who names the setting in a warning.
	redirect := func(who, from, title, to string) error {
		rel, err := t.redirectFile(s, from)
		if err != nil {
			t.Warn(fmt.Sprintf("%s %q: %v", who, from, err))
			return nil
		}
		if other, dup := claimed[rel]; dup {
			t.Warn(fmt.Sprintf("%s %q: %s already redirects there", who, from, other))
			return nil
		}
		if _, err := os.Stat(filepath.Join(t.out, filepath.FromSlash(rel))); err == nil {
			t.Warn(fmt.Sprintf("%s %q: a published page or file is already at that URL", who, from))
			return nil
		}
		claimed[rel] = who
		return target.WriteFile(t.out, rel, t.redirectPage(title, to))
	}
	for _, p := range s.Pages {
		for _, from := range frontmatterStrings(p.Frontmatter[redirectKey]) {
			if err := redirect(p.SourcePath+": "+redirectKey, from, p.Title, p.URL); err != nil {
				return err
			}
		}
	}
	for _, from := range slices.Sorted(maps.Keys(s.Config.Site.Redirects)) {
		to := s.Config.Site.Redirects[from]
		title, dest, err := t.redirectTarget(s, to)
		if err != nil {
			t.Warn(fmt.Sprintf("site.redirects %q: %v", from, err))
			continue
		}
		if err := redirect("site.redirects", from, title, dest); err != nil {
			return err
		}
	}
	return nil
}

// redirectTarget resolves a site.redirects target: a published note (path
// relative to root) or a tag with a tag page.
func (t *Target) redirectTarget(s *target.Site, to string) (title, dest string, err error) {
	if strings.HasPrefix(to, "#") {
		if s.Config.Site.TagHidden(to) {
			return "", "", fmt.Errorf("tag %s is hidden, so it has no page", to)
		}
		for _, k := range []string{strings.ToLower(to), strings.ToLower(strings.TrimPrefix(to, "#"))} {
			if len(s.Tags[k]) > 0 {
				return to, t.scheme.TagURL(to), nil
			}
		}
		return "", "", fmt.Errorf("no published note has tag %s", to)
	}
	src := path.Join(s.Config.Root, to)
	for _, p := range s.Pages {
		if p.SourcePath == src {
			return p.Title, p.URL, nil
		}
	}
	return "", "", fmt.Errorf("%q is not a published note", src)
}

// redirectFile maps an old URL path, relative to base_url ("/old/page/" or
// "/old.html"), to the output file that answers it.
func (t *Target) redirectFile(s *target.Site, from string) (string, error) {
	u, err := url.Parse(from)
	if err != nil || u.Scheme != "" || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(from, "/") {
		return "", fmt.Errorf("must be a path on this site, like /old/page/")
	}
	rel := strings.Trim(u.Path, "/")
	if rel == "" || path.Clean("/"+rel) != "/"+rel {
		return "", fmt.Errorf("must be a clean path below the site root")
	}
	top, _, _ := strings.Cut(rel, "/")
	if reserved[top] || top == ".well-known" || top == suggestSlug && s.Config.Site.Suggest != nil {
		return "", fmt.Errorf("/%s/ is reserved for generated files", top)
	}
	if path.Ext(rel) != "" && !strings.HasSuffix(u.Path, "/") {
		return rel, nil // a file, such as /old.html
	}
	return rel + "/index.html", nil
}

var redirectTmpl = template.Must(template.New("redirect").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Moved: {{.Title}}</title>
<meta name="robots" content="noindex">
<link rel="canonical" href="{{.Canonical}}">
<meta http-equiv="refresh" content="0; url={{.URL}}">
</head>
<body>
<p>This page has moved to <a href="{{.URL}}">{{.Title}}</a>.</p>
</body>
</html>
`))

func (t *Target) redirectPage(title, to string) []byte {
	var b strings.Builder
	_ = redirectTmpl.Execute(&b, struct{ Title, URL, Canonical string }{title, to, t.origin + to}) // writes to a Builder
	return []byte(b.String())
}

// frontmatterStrings reads a frontmatter value that is one string or a list
// of them, without blanks.
func frontmatterStrings(v any) []string {
	var out []string
	switch x := v.(type) {
	case string:
		out = append(out, x)
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	}
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	return slices.DeleteFunc(out, func(s string) bool { return s == "" })
}
