package urls

import "testing"

func TestBasePathOf(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.io/c/": "/c/", "https://x.io/c": "/c/", "https://x.io": "/", "": "/", "/sub": "/sub/",
	} {
		if got := BasePathOf(in); got != want {
			t.Errorf("BasePathOf(%q) = %q want %q", in, got, want)
		}
	}
}

func TestBasicScheme(t *testing.T) {
	s := HTML("/c/")
	if got := s.PageURL("issues/café permit"); got != "/c/issues/caf%C3%A9%20permit/" {
		t.Errorf("page %q", got)
	}
	if got := s.PageURL(""); got != "/c/" {
		t.Errorf("home %q", got)
	}
	if got := s.AssetURL("img/photo.png"); got != "/c/assets/img/photo.png" {
		t.Errorf("asset %q", got)
	}
	if got := s.TagURL("#civic/roads"); got != "/c/tags/civic-roads/" {
		t.Errorf("tag %q", got)
	}
}
