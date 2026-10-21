package core

import "github.com/tivjnb/futi/internal/modulerunner"

// SetLoader подменяет loader, которым Source() грузит futi.yaml.
func SetLoader(o *Options, l modulerunner.ModuleLoader) { o.loader = l }
