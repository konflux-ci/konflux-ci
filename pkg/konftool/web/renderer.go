package web

import (
	"fmt"
	"html/template"
	"io"
	"io/fs"

	"github.com/labstack/echo/v4"
)

// Echo renderer for rendering Go html/template templates.
// All templates should be loaded into a single Template object via
// Parse, ParseFS, etc. and them passed to the renderer.
type renderer struct {
	templates *template.Template
}

// Implement the echo.Renderer interface
func (r *renderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	if r.templates == nil {
		return fmt.Errorf("html/template: %q is undefined", name)
	}
	return r.templates.ExecuteTemplate(w, name, data)
}

// Implement the TemplateLoader interface
func (r *renderer) ParseFS(fsys fs.FS, patterns ...string) (err error) {
	// TODO: This impelmentation does not allow for base/layout templates.
	// see comment in pkg/konftool/webapps.TemplateLoader for details
	if r.templates == nil {
		r.templates, err = template.ParseFS(fsys, patterns...)
	} else {
		_, err = r.templates.ParseFS(fsys, patterns...)
	}
	return
}
