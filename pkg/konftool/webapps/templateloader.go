package webapps

import "io/fs"

// An interface for something that lets an app load its template into.
// It kinkoff mimics the html/template loading API
// We don't include ParseFiles and ParseGlob to discourage accessing the file
// system directly without an injectable abstraction.
type TemplateLoader interface {
	// Load a bunch of templates from a file system. Like with the similar
	// html/template method, the file paths are stripped and the templates are
	// stored with the base file names. If two files with the same name are 
	// parsed, the earlier file is dropped from the collecton.
	ParseFS(fsys fs.FS, patterns ...string) error
	// TODO: Add a separate interface for loading shared and stand-alone
	// templates. If all templates are shared (=loaded into the same
	// html/template object), then we can use the block+override mecahnism 
	// because a template that overrides a block does so for all the templates
	// in the shared collection. What we need instead is a shared collection for
	// all the base (a.k.a layout) templates, and a separate collection for 
	// page templates that may contain block overrides. That separate collection
	// would then keep each of the page tempalets in its own html/template
	// object

	// TODO: Decide what to do about file name extensions. It simpler to keep
	// then in template names, but it may be nicer to strip them
}
