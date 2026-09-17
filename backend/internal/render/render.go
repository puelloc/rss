package render

import (
	"time"

	"github.com/gorilla/feeds"

	"feedforge/internal/transform"
)

func build(r *transform.Result, selfLink string, updated time.Time) *feeds.Feed {
	f := &feeds.Feed{
		Title:       r.Title,
		Link:        &feeds.Link{Href: selfLink},
		Description: r.Description,
		Created:     updated,
		Updated:     updated,
	}
	f.Items = make([]*feeds.Item, 0, len(r.Items))
	for _, it := range r.Items {
		created := it.Published
		if created.IsZero() {
			created = updated
		}
		author := &feeds.Author{}
		if it.Author != "" {
			author = &feeds.Author{Name: it.Author}
		}
		desc := it.Description
		if desc == "" && it.Content != "" {
			desc = it.Content
		}
		f.Items = append(f.Items, &feeds.Item{
			Title:       it.Title,
			Link:        &feeds.Link{Href: it.Link},
			Description: desc,
			Content:     it.Content,
			Id:          it.GUID,
			Author:      author,
			Created:     created,
			Updated:     created,
		})
	}
	return f
}

func RSS(r *transform.Result, selfLink string, updated time.Time) (string, error) {
	return build(r, selfLink, updated).ToRss()
}

func Atom(r *transform.Result, selfLink string, updated time.Time) (string, error) {
	return build(r, selfLink, updated).ToAtom()
}

