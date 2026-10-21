package core

import "github.com/tivjnb/futi/internal/modulerunner"

// SetLoader подменяет loader, которым Source() и Resolve() грузят модули.
func SetLoader(o *Options, l modulerunner.ModuleLoader) { o.loader = l }
