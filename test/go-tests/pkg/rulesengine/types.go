package rulesengine

import "strings"

// File represents a changed file (status and path). Used by utils.GetChangedFiles.
type File struct {
	Status string
	Name   string
}

// Files is a slice of File.
type Files []File

// String returns a comma-separated list of file names.
func (cfs *Files) String() string {
	var names []string
	for _, f := range *cfs {
		names = append(names, f.Name)
	}
	return strings.Join(names, ", ")
}
